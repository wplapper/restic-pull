package metaprint

import (
	// system
	"cmp"
	"context"
	"fmt"
	"golang.org/x/sync/errgroup"
	"io"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	// queue for topological sort algorithm (bfs)
	"github.com/golang-collections/collections/queue"

	// restic
	"github.com/restic/restic/internal/repository"
	"github.com/restic/restic/internal/repository/pack"
	"github.com/restic/restic/internal/restic"
)

type TypeParentChild map[int]struct{}

// define all the maps and slices
type sqlStmt struct {
	mapPackfiles  map[restic.ID]packInfo
	mapIndexEntry map[restic.ID]int
	mapSnapshots  map[restic.ID]int
	contentsMap   map[CompositeKey][]int
	parentChild   map[int]map[int]struct{} // map[parent]Set(children)
	indexMetadata map[restic.ID]struct{}
	snapshots     restic.Snapshots
	verbose       int
	rowCount      int
	startTime     time.Time
	multiOut      io.Writer
	locker 				sync.Mutex
}

type packInfo struct {
	packIDIndex int
	packID      restic.ID
	type_       string
	size        int
	numEntries  int
}

func sqlNew(verbose int, startTime time.Time) *sqlStmt {
	return &sqlStmt{
		mapPackfiles:  map[restic.ID]packInfo{},
		mapIndexEntry: map[restic.ID]int{},
		mapSnapshots:  map[restic.ID]int{},
		contentsMap:   map[CompositeKey][]int{},
		parentChild:   map[int]map[int]struct{}{},
		indexMetadata: map[restic.ID]struct{}{},
		snapshots:     restic.Snapshots{},
		verbose:       verbose,
		//rowCount:      0, // implicit 0
		startTime:     startTime,
	}
}

// called from restic metadata --sql <databaseName>
func RunDatabaseCreation(ctx context.Context, repo *repository.Repository, databaseName string,
	verbose int, startTime time.Time) error {
	var debugOutput *os.File

	sql := sqlNew(verbose, startTime)

	os.Remove(databaseName)
	sqlCmd := exec.Command("/usr/bin/sqlite3", databaseName)
	pipeIn, err := sqlCmd.StdinPipe() // stdin is a io.WriteCloser
	if err != nil {
		fmt.Printf("Can't Create Pipe to STDIN - reason '%v'\n", err)
		return err
	}
	if verbose > 2 {
		debugOutput, err = os.Create("/tmp/copy_of_SQLite_input.sql")
		if err != nil {
			fmt.Printf("Can't Create debugfile - reason '%v'\n", err)
			return err
		}
		sql.multiOut = io.MultiWriter(pipeIn, debugOutput)
	} else {
		sql.multiOut = io.MultiWriter(pipeIn)
	}

	go func() {
		defer pipeIn.Close()
		// write to 'pipeIn' via sql.writeString
		sql.gatherDatabaseData(ctx, repo)
	}()

	// wait for sqlite3 execution to finish
	out, err := sqlCmd.CombinedOutput()
	if err != nil {
		fmt.Printf("CombinedOutput failed - reason '%v'\n", err)
		fmt.Printf("%s\n", out)
		return err
	} else if len(out) > 0 {
		fmt.Printf("*** %s ***\n", out)
	}

	if sql.verbose > 0 {
		fmt.Printf("%8.1f %-20s %7d\n", time.Since(startTime).Seconds(), "create database", sql.rowCount)
	}
	return nil
}

// function to create all database tables in sequence
func (sql *sqlStmt) gatherDatabaseData(ctx context.Context, repo *repository.Repository) error {
	sql.startup()
	sql.createTables()

	// parallel
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		// stream 1
		defer wg.Done()
		err := sql.insertMasterIndex(ctx, repo)
		if err != nil {
			return
		}
	}()

	go func() {
		// stream 2
		defer wg.Done()
		err := sql.insertSnaphots(ctx, repo)
		if err != nil {
			return
		}
		sql.insertSnaphotSummary()
	}()
	wg.Wait()

	// stream 3: depends on master_index (stream 1)
	err := sql.parallelLoadTrees(ctx, repo)
	if err != nil {
		fmt.Printf("parallel error %v\n", err)
		return err
	}


	/*type parPunc func()
	funcSlice := []parFunc{sql.insertPackfiles, sql.insertContent, doSnapBlobs}
	_ = funcSlice
	*/
	var wg2 sync.WaitGroup
	wg2.Add(3)
	go func() {
		// stream 4 = packfiles
		defer wg2.Done()
		// depends on master_index (stream 1)
		sql.insertPackfiles()
	}()

	go func() {
		// stream 5 = content
		defer wg2.Done()
		// depends on parallelLoadTrees (stream 3)
		sql.insertContent()
	}()

	go func() {
		// stream 6, depends on stream 1, 2, 3
		defer wg2.Done()
		sql.doSnapBlobs()
	}()

	// wait for 4, 5, 6 to finish
	wg2.Wait()
	sql.finale()

	return nil
}

// CREATE all defined TABLEs
func (sql *sqlStmt) createTables() {

	sql.locker.Lock()
	for _, createStatement := range CreateTablesSlice {
		sql.writeString(createStatement + ";\n")
	}
	sql.locker.Unlock()
}

// SQLite startup
func (sql *sqlStmt) startup() {

	sql.locker.Lock()
	sql.writeString("PRAGMA foreign_keys=OFF;\n")
	sql.writeString("BEGIN EXCLUSIVE TRANSACTION;\n")
	sql.locker.Unlock()
}

// SQLite finale
func (sql *sqlStmt) finale() {

	sql.locker.Lock()
	sql.writeString("COMMIT;\n")
	sql.locker.Unlock()
}

// generate snapshots and dependent TABLEs (snapshot_summary, snapshot_paths, snapshot_tags)
func (sql *sqlStmt) insertSnaphots(ctx context.Context, repo *repository.Repository, ) error {
	if sql.verbose > 1 {
		fmt.Printf("%8.1f %s\n", time.Since(sql.startTime).Seconds(), "start  snapshots")
	}
	err := repo.List(ctx, restic.SnapshotFile, func(id restic.ID, _ int64) error {
		sn, err := restic.LoadSnapshot(ctx, repo, id)
		if err != nil {
			fmt.Printf("insertSnaphots. skip loading snap record %s! - reason: %v\n", id.Str(), err)
			return err
		}
		sql.snapshots = append(sql.snapshots, sn)

		return nil
	})
	if err != nil {
		fmt.Printf("repo.List failed with %v\n", err)
		return err
	}

	// sort snaps according to sn.Time
	slices.SortStableFunc(sql.snapshots, func(a, b *restic.Snapshot) int {
		return a.Time.Compare(b.Time)
	})

	// map sn.ID() -> to index in sql.snapshots
	sql.mapSnapshots = make(map[restic.ID]int, len(sql.snapshots))
	snapOutput := make([]string, len(sql.snapshots))
	for ix, sn := range sql.snapshots {
		var original string
		if sn.Original != nil {
			original = fmt.Sprintf("X'%s'", sn.Original.String())
		} else {
			original = "NULL"
		}
		sql.mapSnapshots[*sn.ID()] = ix + 1

		snapOutput[ix] = fmt.Sprintf(
			"(%d,X'%s','%s','%s','%s','%s',%d,%d,%s,X'%s','%s')",
			ix+1, sn.ID().String(), sn.ID().Str(),
			sn.Time, sn.Hostname, sn.Username, sn.UID, sn.GID,
			original, sn.Tree.String(), sn.ProgramVersion)
	}

	sql.locker.Lock()
	if sql.verbose > 1 {
		fmt.Printf("%8.1f %-20s\n", time.Since(sql.startTime).Seconds(), "locked snapshots")
	}
	sql.writeString(fmt.Sprintf(
		"INSERT INTO snapshots(id,snap_id,snap_short,time,hostname,username,uid,gid,original,tree,program_version) VALUES\n"))
	sql.writeString(strings.Join(snapOutput, ",\n"))
	sql.writeString(";\n")
	sql.rowCount += len(sql.snapshots)
	sql.locker.Unlock()

	// path information from snapshots
	pathOutput := []string{}
	for is, sn := range sql.snapshots {
		for ip, path := range sn.Paths {
			pathOutput = append(pathOutput, fmt.Sprintf("(%d,%d,'%s')", is+1, ip+1, path))
		}
	}

	sql.locker.Lock()
	sql.writeString(fmt.Sprintf(
		"INSERT INTO snapshot_paths(id, path_index, path) VALUES\n"))
	sql.writeString(strings.Join(pathOutput, ",\n"))
	sql.writeString(";\n")
	sql.rowCount += len(pathOutput)

	sql.locker.Unlock()
	if sql.verbose > 1 {
		fmt.Printf("%8.1f %-20s\n", time.Since(sql.startTime).Seconds(), "unlock snapshots")
	}

	// tag information from snapshots
	tagOutput := []string{}
	for is, sn := range sql.snapshots {
		if sn.Tags != nil {
			for it, tag := range sn.Tags {
				tagOutput = append(tagOutput, fmt.Sprintf("(%d,%d,'%s')", is+1, it+1, tag))
			}
		}
	}

	if len(tagOutput) > 0 {
		sql.locker.Lock()
		sql.writeString(fmt.Sprintf(
			"INSERT INTO snapshot_tags(id, tag_index, tag) VALUES\n"))
		sql.writeString(strings.Join(tagOutput, ",\n"))
		sql.writeString(";\n")
		sql.rowCount += len(tagOutput)
		sql.locker.Unlock()
	}

	return nil
}

// generate rows for TABLE master_index
// create raw data for TABLE packfiles
func (sql *sqlStmt) insertMasterIndex(ctx context.Context, repo *repository.Repository, ) error {
	if sql.verbose > 1 {
		fmt.Printf("%8.1f %s\n", time.Since(sql.startTime).Seconds(), "start  master index")
	}

	packsizeInfo, err := pack.Size(ctx, repo, false)
	if err != nil {
		fmt.Printf("pack.Size failed with %v\n", err)
		return err
	}

	indexLists := []string{}
	highID := 1
	packfileID := 0
	countEntries := 0
	previousPackID := restic.ID{}
	emptyID := restic.ID{}
	err = repo.ListBlobs(ctx, func(blob restic.PackedBlob) {
		// build mapPackfiles
		if _, ok := sql.mapPackfiles[blob.PackID]; !ok {
			packfileID += 1
			if previousPackID != emptyID {
				oldEntry := sql.mapPackfiles[previousPackID]
				sql.mapPackfiles[previousPackID] = packInfo{
					oldEntry.packIDIndex, oldEntry.packID, oldEntry.type_,
					int(packsizeInfo[previousPackID]), countEntries,
				}
				countEntries = 0
			}
			// new entry
			sql.mapPackfiles[blob.PackID] = packInfo{packfileID, blob.PackID, string(blob.Type.String()), 0, 0}
			previousPackID = blob.PackID
		}

		indexLists = append(indexLists, fmt.Sprintf("(%d,X'%s','%s',%d,%d,%d,%d)",
			highID, blob.ID.String(), blob.Type.String(),
			blob.Offset, blob.Length, blob.UncompressedLength, packfileID))
		sql.mapIndexEntry[blob.ID] = highID
		highID += 1
		countEntries += 1
		if blob.Type == restic.TreeBlob {
			sql.indexMetadata[blob.ID] = struct{}{}
		}
	})

	if err != nil {
		fmt.Printf("repo.ListBlobs failed with %v\n", err)
		return err
	}

	// last packfile entry
	oldEntry := sql.mapPackfiles[previousPackID]
	sql.mapPackfiles[previousPackID] = packInfo{
		oldEntry.packIDIndex, oldEntry.packID, oldEntry.type_,
		int(packsizeInfo[previousPackID]), countEntries,
	}
	if sql.verbose > 1 {
		fmt.Printf("%8.1f %-20s %7d\n", time.Since(sql.startTime).Seconds(), "ended  master index", len(sql.indexMetadata))
	}

	sql.locker.Lock()
	if sql.verbose > 1 {
		fmt.Printf("%8.1f %s\n", time.Since(sql.startTime).Seconds(), "locked master index")
	}
	sql.writeString(fmt.Sprintf(
		"INSERT INTO master_index(id,blob,type,offset,length,uncompressedlength, pack__id) VALUES\n"))
	sql.writeString(strings.Join(indexLists, ",\n"))
	sql.writeString(";\n")
	sql.rowCount += len(indexLists)
	sql.locker.Unlock()
	if sql.verbose > 1 {
		fmt.Printf("%8.1f %s\n", time.Since(sql.startTime).Seconds(), "unlock master index")
	}

	return nil
}

// generate TABLE packfiles
func (sql *sqlStmt) insertPackfiles() {
	if sql.verbose > 1 {
		fmt.Printf("%8.1f %s\n", time.Since(sql.startTime).Seconds(), "start  packfiles")
	}

	// sorting will improve database insertion time
	toSort := make([]packInfo, 0, len(sql.mapPackfiles))
	for _, packData := range sql.mapPackfiles {
		toSort = append(toSort, packData)
	}
	slices.SortStableFunc(toSort, func(a, b packInfo) int {
		return cmp.Compare(a.packIDIndex, b.packIDIndex)
	})

	packLists := make([]string, len(sql.mapPackfiles))
	for ix, packData := range toSort {
		packLists[ix] = fmt.Sprintf("(%d,X'%s',%d,'%s',%d)",
			packData.packIDIndex, packData.packID.String(), packData.numEntries, packData.type_, packData.size)
	}
	if sql.verbose > 1 {
		fmt.Printf("%8.1f %-20s %7d\n", time.Since(sql.startTime).Seconds(),
			"ended  packfiles", len(toSort))
	}

	sql.locker.Lock()
	if sql.verbose > 1 {
		fmt.Printf("%8.1f %-20s\n", time.Since(sql.startTime).Seconds(), "locked packfiles")
	}
	sql.writeString("INSERT INTO packfiles(id, packfile_id, blob_count, type, packfile_size) VALUES\n")
	sql.writeString(strings.Join(packLists, ",\n"))
	sql.writeString(";\n")
	sql.rowCount += len(packLists)
	sql.locker.Unlock()
	if sql.verbose > 1 {
		fmt.Printf("%8.1f %-20s\n", time.Since(sql.startTime).Seconds(), "unlock packfiles")
	}

	// drop
	sql.mapPackfiles = nil
}

// fill TABLE contents
func (sql *sqlStmt) insertContent() {

	if sql.verbose > 1 {
		fmt.Printf("%8.1f %s\n", time.Since(sql.startTime).Seconds(), "start  contents")
		//fmt.Printf("mutex CONT %+v\n", m)
	}
	buffer := []string{}
	highID := 1
	for key, dataSlice := range sql.contentsMap {
		metaBlobID := sql.mapIndexEntry[key.MetaBlob]
		position := key.Position
		for offset, dataBlobID := range dataSlice {
			buffer = append(buffer, fmt.Sprintf(
				"(%d,%d,%d,%d,%d)", highID, metaBlobID, position, offset, dataBlobID))
			highID += 1
		}
	}
	if sql.verbose > 1 {
		fmt.Printf("%8.1f %-20s %7d\n", time.Since(sql.startTime).Seconds(),
			"ended  contents", len(buffer))
	}

	sql.locker.Lock()
	if sql.verbose > 1 {
		fmt.Printf("%8.1f %-20s\n", time.Since(sql.startTime).Seconds(), "locked contents")
	}
	sql.writeString("INSERT INTO contents(id, blob__id, position, offset, data__id) VALUES\n")
	sql.writeString(strings.Join(buffer, ",\n"))
	sql.writeString(";\n")
	sql.rowCount += len(buffer)
	sql.locker.Unlock()
	if sql.verbose > 1 {
		fmt.Printf("%8.1f %-20s\n", time.Since(sql.startTime).Seconds(), "unlock contents")
	}

	// drop
	sql.contentsMap = nil
}

// fill TABLE snapshot_summary
func (sql *sqlStmt) insertSnaphotSummary() {
	buffer := []string{}
	for ix, sn := range sql.snapshots {
		if sn.Summary != nil {
			ss := sn.Summary
			row := fmt.Sprintf(
				"(%d,'%s','%s',%d,%d,%d, %d,%d,%d, %d,%d,%d,%d, %d,%d)",
				ix+1, ss.BackupStart, ss.BackupEnd,
				ss.FilesNew, ss.FilesChanged, ss.FilesUnmodified,
				ss.DirsNew, ss.DirsChanged, ss.DirsUnmodified,
				ss.DataBlobs, ss.TreeBlobs, ss.DataAdded, ss.DataAddedPacked,
				ss.TotalFilesProcessed, ss.TotalBytesProcessed)
			buffer = append(buffer, row)
		}
	}

	sql.locker.Lock()
	sql.writeString(fmt.Sprintf(
		`INSERT INTO snapshot_summary(id, backup_start, backup_end,
  files_new, files_changed, files_unmodified,
  dirs_new, dirs_changed, dirs_unmodified,
  data_blobs, tree_blobs, data_added, data_addedpacked,
  total_filesprocessed, total_bytesprocessed) VALUES%s`, "\n"))
	sql.writeString(strings.Join(buffer, ",\n"))
	sql.writeString(";\n")
	sql.rowCount += len(buffer)
	sql.locker.Unlock()
}

// sort parent->child table topologically,
// parent comes before child
// we are using the int's from the database as identifiers
func (sql *sqlStmt) buildTopology(root int) (topoSort []int) {

	// temporary map, used as Set
	visited := map[int]struct{}{}
	visited[root] = struct{}{}

	topoSort = []int{}
	// use 'queue' as a FIFO queue to walk the topology, breadth first
	bQueue := queue.New()
	bQueue.Enqueue(root)
	for bQueue.Len() > 0 {
		parent := bQueue.Dequeue().(int)
		topoSort = append(topoSort, parent)
		for child := range sql.parentChild[parent] {
			if _, ok := visited[child]; !ok {
				bQueue.Enqueue(child)
				visited[child] = struct{}{}
			}
		}
	}

	return topoSort
}

// function to deliver all metablobs for the LoadTree function
func (sql *sqlStmt) deliverTreeBlobs(fn func(restic.ID) error) error {

	for metaID := range sql.indexMetadata {
		fn(metaID)
	}
	return nil
}

// generate nodes table: load all Trees in parallel fashion
func (sql *sqlStmt) parallelLoadTrees(ctx context.Context, repo *repository.Repository) error {

	var lockParentChild sync.Mutex
	var lockContents sync.Mutex
	var lockRows sync.Mutex
	wg, ctx := errgroup.WithContext(ctx)
	chan_tree_blob := make(chan restic.ID)

	// shared local data (row buffer)
	buffer := []string{}
	highID := 1
	// shared data end

	// define the processNodeEntry for each node entry
	processNodeEntry := func(parent restic.ID, position int, node *restic.Node) {
		// handle file data: convert restic.ID to index
		// parentChild for new parent in locked state
		//fmt.Printf("processNodeEntry %s:%4d\n", parent.String()[:12], position)
		parentInt := sql.mapIndexEntry[parent]
		lockParentChild.Lock()
		if _, ok := sql.parentChild[parentInt]; !ok {
			sql.parentChild[parentInt] = map[int]struct{}{}
		}
		lockParentChild.Unlock()

		if node.Type == restic.NodeTypeFile {
			compKey := CompositeKey{parent, position}
			temp := make([]int, len(node.Content))
			for ix, dataBlob := range node.Content {
				temp[ix] = sql.mapIndexEntry[dataBlob]
			}

			lockContents.Lock()
			sql.contentsMap[compKey] = temp
			lockContents.Unlock()
		} else if node.Type == restic.NodeTypeDir {
			lockParentChild.Lock()
			sql.parentChild[parentInt][sql.mapIndexEntry[*node.Subtree]] = struct{}{}
			lockParentChild.Unlock()
		}

		// prepare new entry in 'buffer'
		// these entries can be NULL, need special handling
		var size, links, subtreeID, linkTarget, device string
		if node.Size == 0 {
			size = "NULL"
		} else {
			size = fmt.Sprintf("%d", node.Size)
		}
		if node.Links == 0 {
			links = "NULL"
		} else {
			links = fmt.Sprintf("%d", node.Links)
		}
		if node.Type == "dir" {
			subtreeID = fmt.Sprintf("%d", sql.mapIndexEntry[*node.Subtree])
		} else {
			subtreeID = "NULL"
		}
		if node.LinkTarget == "" {
			linkTarget = "NULL"
		} else {
			linkTarget = node.LinkTarget
			if strings.Index(linkTarget, "'") >= 0 {
				linkTarget = strings.ReplaceAll(linkTarget, "'", "''")
			}
			linkTarget = fmt.Sprintf("'%s'", linkTarget)
		}

		// single quote handling for node.Name
		name := node.Name
		if strings.Index(name, "'") >= 0 {
			name = strings.ReplaceAll(name, "'", "''")
		}

		if node.Type == "chardev" || node.Type == "dev" {
			device = fmt.Sprintf("%d", node.Device)
		} else {
			device = "NULL"
		}

		lockRows.Lock()
		row := fmt.Sprintf(
			"(%d,%d,%d,'%s','%s',%d,'%s','%s','%s',%d,%d,'%s','%s',%d,%d,%s,%s,%s,%s,%s)",
			highID, sql.mapIndexEntry[parent], position,
			name, node.Type, node.Mode,
			node.ModTime, node.AccessTime, node.ChangeTime,
			node.UID, node.GID, node.User, node.Group,
			node.Inode, node.DeviceID,
			size, links, subtreeID, linkTarget, device)

		buffer = append(buffer, row)
		highID += 1
		lockRows.Unlock()
	} // end processNodeEntry(parent, position, node)

	// run the parallel go routines
	if sql.verbose > 1 {
		fmt.Printf("%8.1f %s\n", time.Since(sql.startTime).Seconds(), "start  parallel nodes")
	}
	wg.Go(func() error {
		defer close(chan_tree_blob)

		// this callback function get fed the 'id'
		return sql.deliverTreeBlobs(func(id restic.ID) error {
			select {
			case <-ctx.Done():
				return nil
			case chan_tree_blob <- id:
				return nil
			}
			return nil
		})
	})

	// a worker receives a metablob ID from chan_tree_blob, loads the tree
	// and runs processNodeEntry with id, the node and its position in the slice
	worker := func() error {
		for id := range chan_tree_blob {
			tree, err := restic.LoadTree(ctx, repo, id)
			if err != nil {
				fmt.Printf("LoadTree returned %v\n", err)
				return err
			}
			for position, node := range tree.Nodes {
				// locking happens in processNodeEntry
				processNodeEntry(id, position, node)
			}
		}

		return nil
	}

	// start all these parallel workers
	max_parallel := int(repo.Connections()) + runtime.GOMAXPROCS(0)
	for i := 0; i < max_parallel; i++ {
		wg.Go(worker)
	}

	err := wg.Wait()
	if err != nil {
		fmt.Printf("parallel loadTree failed with %v\n", err)
		return err
	}
	if sql.verbose > 1 {
		fmt.Printf("%8.1f %-20s %7d\n", time.Since(sql.startTime).Seconds(), "ended  parallel node", len(buffer))
	}

	// create SQL statements for TABLE nodes
	sql.locker.Lock()
	sql.writeString(fmt.Sprintf(
		`INSERT INTO nodes(id, blob__id, position, name, type, mode,
    mtime, atime, ctime,
    uid, gid, user, group_name,
    inode, device_id,
    size, links, subtree__id, linktarget, device) VALUES%s`, "\n"))
	sql.writeString(strings.Join(buffer, ",\n"))
	sql.writeString(";\n")
	sql.rowCount += len(buffer)
	sql.locker.Unlock()

	return nil
}

// create all snap_blobs rows
// double loop over all snaps and all metablobs used by this tree
func (sql *sqlStmt) doSnapBlobs() {
	if sql.verbose > 1 {
		fmt.Printf("%8.1f %-20s\n", time.Since(sql.startTime).Seconds(), "start  snap_blobs")
	}
	buffer := []string{}
	for _, sn := range sql.snapshots {
		snapIDInt := sql.mapSnapshots[*sn.ID()]
		for _, metaBlobInt := range sql.buildTopology(sql.mapIndexEntry[*sn.Tree]) {
			buffer = append(buffer, fmt.Sprintf("(%d,%d)", snapIDInt, metaBlobInt))
		}
	}
	if sql.verbose > 1 {
		fmt.Printf("%8.1f %-20s %7d\n", time.Since(sql.startTime).Seconds(), "ended  snap_blobs", len(buffer))
	}

	sql.locker.Lock()
	if sql.verbose > 1 {
		fmt.Printf("%8.1f %-20s\n", time.Since(sql.startTime).Seconds(), "locked snap_blobs")
	}
	sql.writeString("INSERT INTO snap_blobs(snap__id, blob__id) VALUES\n")
	sql.writeString(strings.Join(buffer, ",\n"))
	sql.writeString(";\n")
	sql.rowCount += len(buffer)
	sql.locker.Unlock()
	if sql.verbose > 1 {
		fmt.Printf("%8.1f %-20s\n", time.Since(sql.startTime).Seconds(), "unlock snap_blobs")
	}
}

// write a string to MultiWriter stream sql.multiOut
// by converting string to strings.NewReader and the copying to sql.multiOut.
// writeString has to be called in the locked state. (sql.locker)
func (sql *sqlStmt) writeString(str string) {
	if _, err := io.Copy(sql.multiOut, strings.NewReader(str)); err != nil {
		fmt.Printf("writeString: can't copy %v\n", err)
		return
	}

}
