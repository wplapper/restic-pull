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

	for sn := range FindFilteredSnapshots(ctx, snapshotLister, repo, SnapshotFilter, args) {
		if sn.Summary != nil {
			Verboseff("snap_id %s already has a summary section\n", sn.ID().Str())
			if !opts.DryRun {
				continue
			}
		}

		err := BuildSnapSummary(ctx, repo, sn)
		if err != nil {
			return err
		}

		if !opts.DryRun {
			sn.Original = sn.ID()
			sn.ProgramVersion = version
			new_id, err := restic.SaveSnapshot(ctx, repo, sn)
			if err != nil {
				Printf("Could not create new snap record - reason %v\n", err)
				return err
			}
			Verbosef("saved new snapshot %v\n", new_id.Str())

			// remove current snapshot record?
			if opts.Forget {
				if err = repo.RemoveUnpacked(ctx, restic.SnapshotFile, *sn.ID()); err != nil {
					Printf("RemoveUnpacked could not remove %s - reason %v\n", sn.ID().Str(), err)
					return err
				}
			}
		}
	}

	return nil
}

// walk current snapshot count files and their sizes
func BuildSnapSummary(ctx context.Context, repo *repository.Repository, sn *restic.Snapshot) error {

	TotalFilesProcessed := uint(0)
	TotalBytesProcessed := uint64(0)
	Verboseff("loading current  backup %s from %s for %s:%s\n",
		sn.ID().Str(), sn.Time.Format(time.DateTime), sn.Hostname, sn.Paths[0])

	err := walker.Walk(ctx, repo, *sn.Tree, walker.WalkVisitor{ProcessNode: func(parentTreeID restic.ID, nodepath string, node *restic.Node, err error) error {
		if err != nil {
			Printf("Unable to load tree %s\n ... which belongs to snapshot %s - reason %v\n", parentTreeID, sn.ID().Str(), err)
			return walker.ErrSkipNode
		}

		if node == nil {
			return nil
		} else if node.Type == restic.NodeTypeFile {
			TotalFilesProcessed += 1
			TotalBytesProcessed += node.Size
		}
		return nil
	}})

	if err != nil {
		Printf("walker.Walk fails for snapshot %s - reason %v\n", sn.ID().Str(), err)
		return err
	}

	Verboseff("\n")
	Verboseff("total_files_processed %d\n", TotalFilesProcessed)
	Verboseff("total_bytes_processed %d\n\n", TotalBytesProcessed)

	// build partial snapshot statistics record
	if sn.Summary == nil {
		sn.Summary = &restic.SnapshotSummary{}
	}
	sn.Summary.TotalFilesProcessed = TotalFilesProcessed
	sn.Summary.TotalBytesProcessed = TotalBytesProcessed

	return nil
}
