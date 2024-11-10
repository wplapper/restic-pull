package metaprint

import (
	// system
	"context"
	"encoding/json"
	"fmt"

	// restic
	"github.com/restic/restic/internal/repository"
	"github.com/restic/restic/internal/repository/pack"
	"github.com/restic/restic/internal/restic"
)

// print snapshots - after filter application
func (mpt *metaPrintTables) PrintSnapshotsMeta(snapshots restic.Snapshots) error {
	if mpt.Snapshots {
		if mpt.firstSectionPrinted {
			fmt.Printf(",")
		}
		fmt.Printf(`  "snapshots": [%s`, "\n")

		printed := false
		for _, sn := range snapshots {
			if printed {
				fmt.Printf(",")
			}

			type ToPrint struct {
				SnapID   restic.ID        `json:"snapid"`
				Short    string           `json:"shortid"`
				Snapdata *restic.Snapshot `json:"snapdata"`
			}
			buf, err := json.Marshal(&ToPrint{*sn.ID(), sn.ID().Str(), sn})
			if err != nil {
				return err
			}
			fmt.Printf("    %s\n", string(buf))
			printed = true
		}

		fmt.Printf("  ]\n")
		mpt.firstSectionPrinted = true
	}

	return nil
}

// print all index entries - using repo.ListBlobs so that each blob
// end up as one line of output
// the contents of the index are not saved anywhere - apart from
// being held by the restic system internally
func (mpt *metaPrintTables) PrintIndexMeta(ctx context.Context, repo *repository.Repository) error {
	// need this structure - copied from repository/index/index.go
	repoPacks, err := pack.Size(ctx, repo, false)
	if err != nil {
		fmt.Printf("pack.Size failed. Error is %v\n", err)
		return err
	}

	mpt.packfilesDetail = []packDetail{}
	mapPackfiles := map[restic.ID][]*blobJSON{}
	err = repo.ListBlobs(ctx, func(blob restic.PackedBlob) {
		IndexBlob := &blobJSON{
			ID:                 blob.ID,
			Type:               blob.Type,
			Offset:             blob.Offset,
			Length:             blob.Length,
			UncompressedLength: blob.UncompressedLength,
		}

		// build mapPackfiles
		if _, ok := mapPackfiles[blob.PackID]; !ok {
			mapPackfiles[blob.PackID] = []*blobJSON{}
		}
		// each index blob gets allocated to its encompassing packfile id
		mapPackfiles[blob.PackID] = append(mapPackfiles[blob.PackID], IndexBlob)
	})

	if err != nil {
		fmt.Printf("repo.ListBlobs failed with %v\n", err)
		return err
	}

	for packID, contents := range mapPackfiles {
		mpt.packfilesDetail = append(mpt.packfilesDetail, packDetail{
			PackID:     packID,
			Type:       contents[0].Type,
			Size:       repoPacks[packID],
			NumEntries: len(contents),
		})
	}

	if mpt.IndexData {
		packIDSorted := restic.IDs{}
		for packID := range mapPackfiles {
			packIDSorted = append(packIDSorted, packID)
		}

		if mpt.firstSectionPrinted {
			fmt.Printf(",")
		}
		fmt.Printf(`  "index": [%s`, "\n")

		// print loop
		printedOuter := false
		for _, packID := range packIDSorted {
			if printedOuter {
				fmt.Printf(",")
			}
			printedOuter = true

			fmt.Printf(`    {"packid": "%s", "numblobs": %d, "blobs": [%s`,
				packID, len(mapPackfiles[packID]), "\n")

			//each index entry gets its own output line
			printed := false
			for _, blob := range mapPackfiles[packID] { // blob is a *blobJSON
				if printed {
					fmt.Printf(",")
				}

				buf, _ := json.Marshal(blob)
				fmt.Printf("      %s\n", buf)
				printed = true
			}
			fmt.Printf("    ]}\n")
		}

		fmt.Printf("  ]\n")
		mpt.firstSectionPrinted = true
	}

	return nil
}

// print packfile summary
func (mpt *metaPrintTables) PrintPackfilesSummary() {
	if mpt.firstSectionPrinted {
		fmt.Printf(",")
	}
	fmt.Printf(`  "packfiles": [%s`, "\n")

	printed := false
	for _, pd := range mpt.packfilesDetail {
		if printed {
			fmt.Printf(",")
		}
		printed = true

		buf, _ := json.Marshal(pd) // pd is a packDetail
		fmt.Printf("    %s\n", string(buf))
	}

	fmt.Printf("  ]\n")
	mpt.firstSectionPrinted = true
}
