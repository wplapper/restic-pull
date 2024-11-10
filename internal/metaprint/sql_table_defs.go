package metaprint

// table definitions
/*
type Snapshot struct {
  Time     time.Time `json:"time"`
  Parent   *ID       `json:"parent,omitempty"`
  Tree     *ID       `json:"tree"`
  Paths    []string  `json:"paths"`
  Hostname string    `json:"hostname,omitempty"`
  Username string    `json:"username,omitempty"`
  UID      uint32    `json:"uid,omitempty"`
  GID      uint32    `json:"gid,omitempty"`
  Excludes []string  `json:"excludes,omitempty"`
  Tags     []string  `json:"tags,omitempty"`
  Original *ID       `json:"original,omitempty"`

  ProgramVersion string           `json:"program_version,omitempty"`
  Summary        *SnapshotSummary `json:"summary,omitempty"`

  id *ID // plaintext ID, used during restore
}

type SnapshotSummary struct {
  BackupStart time.Time `json:"backup_start"`
  BackupEnd   time.Time `json:"backup_end"`

  // statistics from the backup json output
  FilesNew            uint   `json:"files_new"`
  FilesChanged        uint   `json:"files_changed"`
  FilesUnmodified     uint   `json:"files_unmodified"`
  DirsNew             uint   `json:"dirs_new"`
  DirsChanged         uint   `json:"dirs_changed"`
  DirsUnmodified      uint   `json:"dirs_unmodified"`
  DataBlobs           int    `json:"data_blobs"`
  TreeBlobs           int    `json:"tree_blobs"`
  DataAdded           uint64 `json:"data_added"`
  DataAddedPacked     uint64 `json:"data_added_packed"`
  TotalFilesProcessed uint   `json:"total_files_processed"`
  TotalBytesProcessed uint64 `json:"total_bytes_processed"`
}

*/
var CreateTablesSlice = []string{
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
)`,

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
)`,

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
)`,

	`CREATE TABLE packfiles (
    id INTEGER PRIMARY KEY,             -- the primary key
    packfile_id BLOB NOT NULL,          -- the packfile ID, UNIQUE INDEX
    blob_count INTEGER NOT NULL,        -- number of blobs in packfile
    type TEXT NOT NULL,                 -- type of packfile data/tree
    packfile_size INTEGER NOT NULL      -- data length of packfile
    --CONSTRAINT ux_pfiles_pack           UNIQUE(packfile_id)
)`,

	/*
	   // Node is a file, directory or other item in a backup.
	   type Node struct {
	     Name       string      `json:"name"`
	     Type       NodeType    `json:"type"`
	     Mode       os.FileMode `json:"mode,omitempty"`
	     ModTime    time.Time   `json:"mtime,omitempty"`
	     AccessTime time.Time   `json:"atime,omitempty"`
	     ChangeTime time.Time   `json:"ctime,omitempty"`
	     UID        uint32      `json:"uid"`
	     GID        uint32      `json:"gid"`
	     User       string      `json:"user,omitempty"`
	     Group      string      `json:"group,omitempty"`
	     Inode      uint64      `json:"inode,omitempty"`
	     DeviceID   uint64      `json:"device_id,omitempty"` // device id of the file, stat.st_dev, only stored for hardlinks
	     Size       uint64      `json:"size,omitempty"`
	     Links      uint64      `json:"links,omitempty"`
	     LinkTarget string      `json:"linktarget,omitempty"`
	     // implicitly base64-encoded field. Only used while encoding, `linktarget_raw` will overwrite LinkTarget if present.
	     // This allows storing arbitrary byte-sequences, which are possible as symlink targets on unix systems,
	     // as LinkTarget without breaking backwards-compatibility.
	     // Must only be set of the linktarget cannot be encoded as valid utf8.
	     //ExtendedAttributes []ExtendedAttribute                      `json:"extended_attributes,omitempty"`
	     //GenericAttributes  map[GenericAttributeType]json.RawMessage `json:"generic_attributes,omitempty"`
	     Device             uint64                                   `json:"device,omitempty"` // in case of Type == "dev", stat.st_rdev
	     //Content            IDs                                      `json:"content"`
	     Subtree            *ID                                      `json:"subtree,omitempty"`
	   }
	*/
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
)`,

	`CREATE TABLE contents (
    id INTEGER PRIMARY KEY,             -- primary key
    blob__id INTEGER NOT NULL,          -- ptr to idd_file,        UNIQUE KEY, part 1
    position INTEGER NOT NULL,          -- position in file slice, UNIQUE KEY, part 2
    offset   INTEGER NOT NULL,          -- offset into contents    UNIQUE KEY, part 3
    data__id INTEGER NOT NULL,          -- ptr to ixr.id, INDEX
    FOREIGN KEY(data__id)               REFERENCES master_index(id),
    FOREIGN KEY(blob__id)               REFERENCES master_index(id)
    --CONSTRAINT ux_cont_idblof           UNIQUE(blob__id, position, offset)
)`,

	// tables WITHOUT ROWID, because of composite PRIMARY KEYs
	`CREATE TABLE snapshot_paths (
    id INTEGER NOT NULL,                -- primary key par, same as snapshots.id
    path_index INTEGER NOT NULL,        -- index into path slice
    path TEXT NOT NULL,
    PRIMARY KEY(id, path_index)         -- UNIQUE
) WITHOUT ROWID`,

	`CREATE TABLE snapshot_tags (
    id INTEGER NOT NULL,                -- primary key p1, same as snapshots.id
    tag_index INTEGER NOT NULL,         -- primary key part 2
    tag TEXT NOT NULL,
    PRIMARY KEY(id, tag_index)          -- UNIQUE
) WITHOUT ROWID`,

	`CREATE TABLE snap_blobs (            -- primary key only table
    snap__id INTEGER NOT NULL,          -- the snap_id pointer
    blob__id INTEGER NOT NULL,          -- the blob pointer
    PRIMARY KEY(snap__id, blob__id),    -- UNIQUE
    FOREIGN KEY(snap__id)               REFERENCES snapshots(id),
    FOREIGN KEY(blob__id)               REFERENCES master_index(id)
) WITHOUT ROWID`,
}
