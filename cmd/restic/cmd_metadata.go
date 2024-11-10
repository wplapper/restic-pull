package main

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
*/

import (
	// system
	"context"
	"time"

	// argparse
	"github.com/spf13/cobra"

	// restic
	"github.com/restic/restic/internal/metaprint"
	"github.com/restic/restic/internal/restic"
)

var cmdMetaData = &cobra.Command{
	Use:   "metadata [flags] args",
	Short: "extract metadata from repository",
	Long: `extract metadata from repository

All output is JSON based. There is no intention to format this output
in more human friendly way. Use pipe to jq to show output more suitable
for human consumption.

The following options can be used:
 --snapshot,      -S: show selected snapshots
 --index,         -I: show all index data and containing packfile
 --file,          -F: show file meta data via chosen snapshots
 --parent-child,  -P: show parent to child relationship via chosen snapshots
 --datablob,      -D: show datablob details (snaoId, metablob, position in file table, offset in contents list)
 --packfiles      -Y  show packfile summary
 --snap-meta      -T  show all metablobs belonging to a given snapshot
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

  snap-meta: "snap_meta": [
    {
      "snapid": "<snapid 1>",
      "numblobs": <n 1>,
      "blobids": [
        "<blob 1>",
      ]
		}, ... ]


EXIT STATUS
===========

Exit status is 0 if the command was successful.
Exit status is 1 if there was any error.
Exit status is 10 if the repository does not exist.
Exit status is 11 if the repository is already locked.
Exit status is 12 if the password is incorrect.
`,
	GroupID:           cmdGroupDefault,
	DisableAutoGenTag: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runMetaData(cmd.Context(), metaDataOptions, globalOptions, args)
	},
}

type MetaDataOptions struct {
	AllData bool
	metaprint.MetaPrintOpts
	restic.SnapshotFilter
}

var metaDataOptions MetaDataOptions

func init() {
	cmdRoot.AddCommand(cmdMetaData)

	f := cmdMetaData.Flags()
	initMultiSnapshotFilter(f, &metaDataOptions.SnapshotFilter, true)
	f.BoolVarP(&metaDataOptions.Snapshots, "snapshot", "S", false, "produce snapshot metadata")
	f.BoolVarP(&metaDataOptions.IndexData, "index", "I", false, "produce index metadata")
	f.BoolVarP(&metaDataOptions.FileData, "file", "F", false, "produce file metadata")
	f.BoolVarP(&metaDataOptions.ParentChild, "parent-child", "P", false, "produce parent to child relationship table (global)")
	f.BoolVarP(&metaDataOptions.SnapMeta, "snap-meta", "T", false, "snapid to metablob table")
	f.BoolVarP(&metaDataOptions.DataBlobs, "data-blob", "D", false, "produce data blob details")
	f.BoolVarP(&metaDataOptions.Packfiles, "packfiles", "Y", false, "produce packfile summary")
	f.BoolVarP(&metaDataOptions.AllData, "all", "A", false, "produce all metadata")
	f.StringVar(&metaDataOptions.Sql, "sql", "", "generate SQlite insert statementsin <database>")
}

// main meta data extractor
func runMetaData(ctx context.Context, opts MetaDataOptions, gopts GlobalOptions, args []string) error {

	var err error
	snapshots := restic.Snapshots{}
	forcedFileData := false
	forcedIndex := false
	startTime := time.Now()

	ctx, repo, unlock, err := openWithReadLock(ctx, gopts, false)
	if err != nil {
		return err
	}
	defer unlock()
	if gopts.Verbose > 0 {
		Warnf("repository %s open\n", gopts.Repo)
	}
	if gopts.Verbose > 1 {
		Warnf("%8.1f %s\n", time.Since(startTime).Seconds(), "repository open")
	}

	err = repo.LoadIndex(ctx, nil)
	if err != nil {
		return err
	}
	if gopts.Verbose > 1 {
		Warnf("%8.1f %s\n", time.Since(startTime).Seconds(), "master index loaded")
	}

	// flag manipulation, so that each section can be outputted individually
	if opts.IndexData || opts.Packfiles {
		forcedIndex = true
	}
	if opts.ParentChild || opts.SnapMeta || opts.DataBlobs || opts.FileData {
		forcedFileData = true
	}

	if opts.AllData {
		opts.Snapshots = true
		opts.IndexData = true
		opts.FileData = true
		opts.ParentChild = true
		opts.SnapMeta = true
		opts.DataBlobs = true
		opts.Packfiles = true

		forcedFileData = true
		forcedIndex = true
	}

	for sn := range FindFilteredSnapshots(ctx, repo, repo, &opts.SnapshotFilter, args) {
		snapshots = append(snapshots, sn)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if gopts.Verbose > 1 {
		Warnf("%8.1f %s\n", time.Since(startTime).Seconds(), "FindFilteredSnapshots")
	}

	if len(opts.Sql) == 0 {
		Printf("{\n")

		mpt := metaprint.New(opts.MetaPrintOpts)
		if opts.Snapshots {
			err = mpt.PrintSnapshotsMeta(snapshots)
			if err != nil {
				return err
			}
		}

		if forcedIndex {
			err = mpt.PrintIndexMeta(ctx, repo)
			if err != nil {
				return err
			}
		}

		if forcedFileData {
			err = mpt.PrintFileMeta(ctx, repo, snapshots)
			if err != nil {
				return err
			}
		}

		if opts.DataBlobs {
			mpt.PrintCompositeKeyDataTable()
		}

		if opts.ParentChild {
			mpt.PrintParentChild()
		}

		if opts.SnapMeta {
			mpt.PrintTopology(snapshots)
		}

		if opts.Packfiles {
			mpt.PrintPackfilesSummary()
		}

		Printf("}\n")
	} else {
		return metaprint.RunDatabaseCreation(ctx, repo, opts.Sql, gopts.Verbose, startTime)
	}

	return nil
}
