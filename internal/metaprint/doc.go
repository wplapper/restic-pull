// Package restic is the top level package for the restic backup program,
// please see https://github.com/restic/restic for more information.
//
// This package exposes the main objects that are handled in metaprint.
package metaprint
/*
  show metadata from restic repository.
  Show:
		- snapshots
		- index entries
		- file metadata
		- parent to child relationship a global table
		- metadata set for each snap
		- datablob details
		- packfiles

	All output can be created by referencing indirection tables for
	snapshots, index extries and packfile entries

	Input can be selected by the standard restic.SnapshotFilter
	also snapshot IDs can be given as arguments.

	If you want to see the output in a more pleasant format, pipe
	generated output through 'jq'

	It is questionable whether sorting is helpful or useful. If the JSOn output
	is used for further processsing, then sorting might not help at all.

	If you look at the output with the human eye, sorted output will help to
	find things more easily. However it increases the code base, in some places
	even considerably.

The following options can be used:
 --snapshot,      -S: show selected snapshots
 --index,         -I: show all index data and containg packfile
 --file,          -F: show file meta data via chosen snapshots
 --parent-child,  -P: show parent to child relationship via chosen snapshots, one table
 --datablob,      -D: show datablob details (snaoId, metablob, position in file table, offset in contents list)
 --packfiles      -Y  show packfile summary
 --snap-meta      -T  show all metablobs belonging to a snapshot
 --all,           -A: activate all of the above flags (snapshot,index,file,datablob,parent-child,packfiles)

Format of selected output:

  snapshots: "snapshots": [
    {
      "snapID": "<snapid 1>",
      "short": "<short snapid 1>"
      "snapdata": {
        normal JSON output for a snapshot 1
      }
    }, ... ]

  index: "index": [
		{
			"packid": "<packid 1>",
			"blobs": [
				<normal JSON ouput for an index entry 1>,
			]
		}, ... ]


  file: "file-meta-data": [
    {
      "id": "<metablob 1>",
      "path": "<directory path for metablob 1>",
      "nodes": <normal JSON output for a metablob 1>
    }, ... ]

  parent-child: "parent-child-map": [
    {
      "parent": "<parent 1 metablob>",
      "children": [
        <child 1 metablob>,
      ]
    }, ... ]

   datablob: "datablob_table": [
    {
      "composite_key": {
        "metablob": "<metablob 1>",
        "position": <parent 1>
      },
      "contents": [
        "<datablob 1>", ...
      ]
    }, ... ]

  packfiles: "packfiles": [
    {
      "packid": "<packid 1>",
      "type": "<type 1>",
      "length": <length 1>,
      "num-entries": <num-entries 1>
    }, ... ]

Table definitions for the SQLite database
	`CREATE TABLE snapshots (
    id INTEGER PRIMARY KEY,             -- ID of snapshots row
    snap_id         BLOB NOT NULL,      -- snap ID, UNIQUE INDEX
    snap_short      TEXT NOT NULL,      -- snap ID, first 8 character as string
    time            TEXT NOT NULL,      -- time of snap, truncated to seconds
    hostname        TEXT NOT NULL,      -- host which is to be backep up
    username        TEXT NOT NULL,      -- user name of backup
    uid             INTEGER NOT NULL,   -- uid of backup
    gid             INTEGER NOT NULL,   -- group id of backup
    original        BLOB,               -- parent?
    tree            BLOB NOT NULL,      -- the root of the snap
    program_version TEXT                -- version of restic
    --CONSTRAINT ux_snapshots_id,         UNIQUE(snap_id),
    --CONSTRAINT ux_snapshots_short       UNIQUE(snap_short)
)`

	`CREATE TABLE snapshot_summary (
    id                   INTEGER PRIMARY KEY, -- =snapshors.id
    backup_start         TEXT,
    backup_end           TEXT,
    files_new            INTEGER,             -- files section
    files_changed        INTEGER,
    files_unmodified     INTEGER,
    dirs_new             INTEGER,             -- directory section
    dirs_changed         INTEGER,
    dirs_unmodified      INTEGER,
    data_blobs           INTEGER,             -- count of blobs
    tree_blobs           INTEGER,
    data_added           INTEGER,             -- how much added
    data_addedpacked     INTEGER,
    total_filesprocessed INTEGER,             -- overall counters
    total_bytesprocessed INTEGER
)`

	`CREATE TABLE master_index (
    id INTEGER PRIMARY KEY,             -- the primary key
    blob BLOB NOT NULL,                 -- the id, UNIQUE INDEX, [32]byte
    type TEXT NOT NULL,                 -- type tree / data
    offset INTEGER NOT NULL,            -- offset in metapack
    length INTEGER NOT NULL,            -- length of blob (compressed)
    uncompressedlength INTEGER,         -- uncompressed length
    pack__id INTEGER NOT NULL,          -- back ptr to packflies, INDEX
    FOREIGN KEY(pack__id)               REFERENCES packfiles(id)
    --CONSTRAINT ux_index_repo_blob       UNIQUE(blob)
)`

	`CREATE TABLE packfiles (
    id INTEGER PRIMARY KEY,             -- the primary key
    packfile_id BLOB NOT NULL,          -- the packfile ID, UNIQUE INDEX
    blob_count INTEGER NOT NULL,        -- number of blobs in packfile
    type TEXT NOT NULL,                 -- type of packfile data/tree
    packfile_size INTEGER NOT NULL      -- data length of packfile
    --CONSTRAINT ux_pfiles_pack           UNIQUE(packfile_id)
)`

	`CREATE TABLE nodes (
    id INTEGER PRIMARY KEY,
    blob__id INTEGER NOT NULL,          -- back ptr to master_index UNIQUE KEY, part 1
    position INTEGER NOT NULL,          -- offset into slice        UNIQUE KEY, part 2
    name TEXT NOT NULL,                 -- name
    type TEXT NOT NULL,
    mode INTEGER NOT NULL,
    mtime TEXT NOT NULL,
    atime TEXT NOT NULL,
    ctime TEXT NOT NULL,
    uid INTEGER NOT NULL,
    gid INTEGER NOT NULL,
    user TEXT NOT NULL,
    group_name TEXT NOT NULL,
    inode INTEGER NOT NULL,
    device_id INTEGER,
    size INTEGER,                       -- is NULL for dir, empty file, symlink
    links INTEGER,                      -- is NULL for dir
    subtree__id INTEGER,                -- is NULL for      file, symlink
    linktarget TEXT,                    -- is NULL for dir, file
    device INTEGER,                     -- is NOT NULL for type='chardev' and 'dev'
    FOREIGN KEY(blob__id)               REFERENCES master_index(id)
    --CONSTRAINT ux_mds_id_blob           UNIQUE(blob__id, position)
)`

	`CREATE TABLE contents (
    id INTEGER PRIMARY KEY,             -- primary key
    blob__id INTEGER NOT NULL,          -- ptr to idd_file,        UNIQUE KEY, part 1
    position INTEGER NOT NULL,          -- position in file slice, UNIQUE KEY, part 2
    offset   INTEGER NOT NULL,          -- offset into contents    UNIQUE KEY, part 3
    data__id INTEGER NOT NULL,          -- ptr to ixr.id, INDEX
    FOREIGN KEY(data__id)               REFERENCES master_index(id),
    FOREIGN KEY(blob__id)               REFERENCES master_index(id)
    --CONSTRAINT ux_cont_idblof           UNIQUE(blob__id, position, offset)
)`

	`CREATE TABLE snapshot_paths (
    id INTEGER NOT NULL,                -- primary key part 1, same as snapshots.id
    path_index INTEGER NOT NULL,        -- index into path slice
    path TEXT NOT NULL,
    PRIMARY KEY(id, path_index)
) WITHOUT ROWID`

	`CREATE TABLE snapshot_tags (
    id INTEGER NOT NULL,                -- primary key p1, same as snapshots.id
    tag_index INTEGER NOT NULL,         -- primary key part 2
    tag TEXT NOT NULL,
    PRIMARY KEY(id, tag_index)
) WITHOUT ROWID`

	`CREATE TABLE snap_blobs (            -- primary key only table
    snap__id INTEGER NOT NULL,          -- the snap_id pointer
    blob__id INTEGER NOT NULL,          -- the blob pointer
    PRIMARY KEY(snap__id, blob__id),    -- UNIQUE
    FOREIGN KEY(snap__id)               REFERENCES snapshots(id),
    FOREIGN KEY(blob__id)               REFERENCES master_index(id)
) WITHOUT ROWID`

*/
