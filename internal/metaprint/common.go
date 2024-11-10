package metaprint

import (
	// system
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	// queue for topological sort algorithm (bfs)
	"github.com/golang-collections/collections/queue"

	// restic
	"github.com/restic/restic/internal/repository"
	"github.com/restic/restic/internal/restic"
)

type CompositeKey struct {
	MetaBlob restic.ID `json:"metablob"`
	Position int       `json:"position"`
}

type packDetail struct {
	PackID     restic.ID       `json:"packid"`
	Type       restic.BlobType `json:"type"`
	Size       int64           `json:"size"`
	NumEntries int             `json:"numentries"`
}

// copied from internal/repository/index/index.go
type blobJSON struct {
	ID                 restic.ID       `json:"id"`
	Type               restic.BlobType `json:"type"`
	Offset             uint            `json:"offset"`
	Length             uint            `json:"length"`
	UncompressedLength uint            `json:"uncompressed_length,omitempty"`
}

type MetaPrintOpts struct {
	Snapshots   bool
	IndexData   bool
	FileData    bool
	ParentChild bool
	FlatTopo    bool
	DataBlobs   bool
	Packfiles   bool
	Sql         string
}

type metaPrintTables struct {
	Snapshots           bool
	IndexData           bool
	FileData            bool
	ParentChild         bool
	FlatTopo            bool
	DataBlobs           bool
	Packfiles           bool
	Sql                 string
	firstSectionPrinted bool

	packfilesDetail []packDetail                // packfilesDetail are a list, could be a Set
	parentChild     map[restic.ID]restic.IDSet  // parent-> children (children are a Set, not a list)
	contentsMap     map[CompositeKey]restic.IDs // move contents off to separate table
}

// copied from internal/restic/node.go
type NodeCopy struct {
	Name       string          `json:"name"`
	Type       restic.NodeType `json:"type"`
	Mode       os.FileMode     `json:"mode,omitempty"`
	ModTime    time.Time       `json:"mtime,omitempty"`
	AccessTime time.Time       `json:"atime,omitempty"`
	ChangeTime time.Time       `json:"ctime,omitempty"`
	UID        uint32          `json:"uid"`
	GID        uint32          `json:"gid"`
	User       string          `json:"user,omitempty"`
	Group      string          `json:"group,omitempty"`
	Inode      uint64          `json:"inode,omitempty"`
	DeviceID   uint64          `json:"device_id,omitempty"` // device id of the file, stat.st_dev, only stored for hardlinks
	Size       uint64          `json:"size,omitempty"`
	Links      uint64          `json:"links,omitempty"`
	LinkTarget string          `json:"linktarget,omitempty"`
	// implicitly base64-encoded field. Only used while encoding, `linktarget_raw` will overwrite LinkTarget if present.
	// This allows storing arbitrary byte-sequences, which are possible as symlink targets on unix systems,
	// as LinkTarget without breaking backwards-compatibility.
	// Must only be set of the linktarget cannot be encoded as valid utf8.
	LinkTargetRaw []byte `json:"linktarget_raw,omitempty"`
	//ExtendedAttributes []ExtendedAttribute                      `json:"extended_attributes,omitempty"`
	//GenericAttributes  map[GenericAttributeType]json.RawMessage `json:"generic_attributes,omitempty"`
	Device uint64 `json:"device,omitempty"` // in case of Type == "dev", stat.st_rdev
	//Content          restic.IDs                                      `json:"content"`
	Subtree   *restic.ID    `json:"subtree,omitempty"`
	ContIndex *CompositeKey `json:"content_index,omitempty"`
	//Error string `json:"error,omitempty"`
	//Path string `json:"-"`
}

func New(mpo MetaPrintOpts) *metaPrintTables {
	mpt := &metaPrintTables{
		Snapshots:   mpo.Snapshots,
		IndexData:   mpo.IndexData,
		FileData:    mpo.FileData,
		ParentChild: mpo.ParentChild,
		FlatTopo:    mpo.FlatTopo,
		DataBlobs:   mpo.DataBlobs,
		Packfiles:   mpo.Packfiles,
		Sql:         mpo.Sql,

		firstSectionPrinted: false,

		parentChild: map[restic.ID]restic.IDSet{},
		contentsMap: map[CompositeKey]restic.IDs{},
	}
	return mpt
}

// print the metadata for one snapshot, recurse into subdirectories
func (mpt *metaPrintTables) walkMetaTree(ctx context.Context, repo *repository.Repository,
	parent restic.ID, snapId *restic.ID, nodepath string, printBuffer *[]string, commonNames []string) error {
	// if the tree has already been processed, return back
	if _, ok := mpt.parentChild[parent]; ok {
		return nil
	}
	mpt.parentChild[parent] = restic.IDSet{}

	// load the tree
	tree, err := restic.LoadTree(ctx, repo, parent)
	if err != nil {
		fmt.Printf("LoadTree failed: %v\n", err)
		return err
	}

	fileMetaData := make([]string, 0, len(tree.Nodes))
	for position, node := range tree.Nodes {
		compKey := CompositeKey{parent, position}

		// prepare nodeCopy: copy common field values
		nodeCopy := &NodeCopy{}
		src := reflect.ValueOf(node).Elem()
		dst := reflect.ValueOf(nodeCopy).Elem()
		for _, name := range commonNames {
			dst.FieldByName(name).Set(src.FieldByName(name))
		}

		// copy contents, if any
		if node.Type == restic.NodeTypeFile {
			mpt.contentsMap[compKey] = node.Content[:]
			node.Content = nil
			nodeCopy.ContIndex = &compKey
		}

		buf, _ := json.Marshal(nodeCopy)
		fileMetaData = append(fileMetaData, fmt.Sprintf("    %s\n", string(buf)))
	}

	if len(fileMetaData) > 0 {
		*printBuffer = append(*printBuffer, fmt.Sprintf(`  {"parent": "%s", "path": "%s", "numnodes": %d, "nodes": [%s%s]}`,
			parent.String(), nodepath, len(fileMetaData), "\n", strings.Join(fileMetaData, ",")))
	}

	// recurse into subtrees, if any
	for _, node := range tree.Nodes {
		if node.Type == restic.NodeTypeDir {
			subtree := *node.Subtree
			mpt.parentChild[parent][subtree] = struct{}{}

			// calculate directory path
			var newPath string
			if nodepath == "/" {
				newPath = "/" + node.Name
			} else if subtree.String()[:12] == "ac08ce34ba4f" { // empty subdirectory
				newPath = "<empty-subdirectory>"
			} else {
				newPath = nodepath + "/" + node.Name
			}

			// recurse
			mpt.walkMetaTree(ctx, repo, subtree, snapId, newPath, printBuffer, commonNames)
		}
	}

	return nil
}

// sort parent->child table topologically,
// parent comes before child
func (mpt *metaPrintTables) buildTopology(root *restic.ID) (topoSort restic.IDs) {

	visited := restic.IDSet{}
	topoSort = restic.IDs{}

	// use 'queue' as a FIFO queue to walk the topology, breadth first
	bQueue := queue.New()
	bQueue.Enqueue(*root)
	visited.Insert(*root) // not needed, but we are doing for completeness
	for bQueue.Len() > 0 {
		parent := bQueue.Dequeue().(restic.ID)
		topoSort = append(topoSort, parent)
		for child := range mpt.parentChild[parent] {
			if !visited.Has(child) {
				bQueue.Enqueue(child)
				visited.Insert(child)
			}
		}
	}

	return topoSort
}

// use the reflect packagre to extract field names from `restic.Node` and `NodeCopy`
func findCommonNames() []string {
	orgNamesSet := map[string]struct{}{}
	cpyNamesSet := map[string]struct{}{}

	sOrg := reflect.ValueOf(restic.Node{})
	typeOfOrg := sOrg.Type()
	sCpy := reflect.ValueOf(NodeCopy{})
	typeOfCpy := sCpy.Type()

	for i := 0; i < sOrg.NumField(); i++ {
		orgNamesSet[typeOfOrg.Field(i).Name] = struct{}{}
	}
	for i := 0; i < sCpy.NumField(); i++ {
		cpyNamesSet[typeOfCpy.Field(i).Name] = struct{}{}
	}

	// common names = intersection orgNamesSet & cpyNamesSet
	commonNames := []string{}
	for name := range cpyNamesSet {
		if _, ok := orgNamesSet[name]; ok {
			commonNames = append(commonNames, name)
		}
	}

	return commonNames
}

// dump messages onto os.Stderr
func Warnf(format string, args ...interface{}) {
	_, err := fmt.Fprintf(os.Stderr, format, args...)
	if err != nil {
		panic("unable to write to stderr")
	}
}
