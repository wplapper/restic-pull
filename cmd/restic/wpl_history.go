package main

/*
Execute snapshot-summary function: build the Snapshot.Summary section if
it does not exist.

In DryRun mode the calculations will be done, but no changes will be made.

Specify extra verbosity to see the numbers on stdout.

If you want to compare numbers with a snapshot which has already statistics
data attached, run:
restic -r <repo> rewrite -svn <snap_id>
*/

import (
	// system
	"context"
	"time"

	// restic
	"github.com/restic/restic/internal/repository"
	"github.com/restic/restic/internal/restic"
	"github.com/restic/restic/internal/walker"
)

// run rewrite --snapshot-summary subcommand
func rewriteSnapshotSummary(ctx context.Context, repo *repository.Repository,
	gopts GlobalOptions, snapshotLister restic.Lister, SnapshotFilter *restic.SnapshotFilter,
	args []string, opts RewriteOptions) error {

	for currentBackup := range FindFilteredSnapshots(ctx, snapshotLister, repo, SnapshotFilter, args) {
		if currentBackup.Summary != nil && currentBackup.Summary.TotalBytesProcessed > 0 {
			Verboseff("snap_id %s already has a summary section\n", currentBackup.ID().Str())
			if !opts.DryRun {
				continue
			}
		}

		ss := &restic.SnapshotSummary{}
		// currentBackup gets a Summary section attached
		err := BuildSnapSummary(ctx, repo, currentBackup, currentBackup.Tree, ss)
		if err != nil {
			return err
		}

		if !opts.DryRun {
			currentBackup.Original = currentBackup.ID()
			new_id, err := restic.SaveSnapshot(ctx, repo, currentBackup)
			if err != nil {
				Printf("Could not create new snap record - reason %v\n", err)
				return err
			}
			Verbosef("saved new snapshot %v\n", new_id.Str())

			// remove current snapshot record?
			if opts.Forget {
				if err = repo.RemoveUnpacked(ctx, restic.SnapshotFile, *currentBackup.ID()); err != nil {
					Printf("RemoveUnpacked could not remove %s - reason %v\n", currentBackup.ID().Str(), err)
					return err
				}
			}
		}
	}

	return nil
}

// walk current snapshot, expect empty or partially filled restic.SnapshotSummary entry
func BuildSnapSummary(ctx context.Context, repo *repository.Repository,
	currentBackup *restic.Snapshot, currentTree *restic.ID, ss *restic.SnapshotSummary) error {

	totalFilesProcessed := uint(0)
	totalBytesProcessed := uint64(0)
	Verboseff("loading current  backup %s from %s for %s:%s\n",
		currentBackup.ID().Str(),
		currentBackup.Time.Format(time.DateTime),
		currentBackup.Hostname, currentBackup.Paths[0])

	// walk tree for current snapshot
	err := walker.Walk(ctx, repo, *currentTree,
		walker.WalkVisitor{ProcessNode: func(parentTreeID restic.ID, nodepath string, node *restic.Node, err error) error {
			if err != nil {
				Printf("Unable to load tree %s\n ... which belongs to snapshot %s - reason %v\n", parentTreeID, currentBackup.ID().Str(), err)
				return walker.ErrSkipNode
			}

			if node == nil {
				return nil
			} else if node.Type == "file" {
				totalFilesProcessed += 1
				totalBytesProcessed += node.Size
			}
			return nil
		}})

	if err != nil {
		Printf("walker.Walk does not want to run for snapshot %s - reason %v\n", currentBackup.ID().Str(), err)
		return err
	}

	Verboseff("\n")
	Verboseff("total_files_processed %d\n", totalFilesProcessed)
	Verboseff("total_bytes_procTotalFilesProcessedessed %d\n\n", totalBytesProcessed)

	// build partial snapshot statistics record
	ss.TotalFilesProcessed = totalFilesProcessed
	ss.TotalBytesProcessed = totalBytesProcessed

	// and attach statistics to current snapshot
	currentBackup.Summary = ss

	return nil
}
