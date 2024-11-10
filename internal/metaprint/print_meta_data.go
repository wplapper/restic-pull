package metaprint

import (
	"context"
	"encoding/json"
	"fmt"

	// restic
	"github.com/restic/restic/internal/repository"
	"github.com/restic/restic/internal/restic"
)

// print meta data function by snapshot
func (mpt *metaPrintTables) PrintFileMeta(ctx context.Context, repo *repository.Repository,
	snapshots restic.Snapshots) error {

	commonNames := findCommonNames()
	printBuffer := []string{}
	for _, sn := range snapshots {
		err := mpt.walkMetaTree(ctx, repo, *sn.Tree, sn.ID(), "/", &printBuffer, commonNames)
		if err != nil {
			return err
		}
	}

	// remove entries with no children
	for parent, childSet := range mpt.parentChild {
		if len(childSet) == 0 {
			delete(mpt.parentChild, parent)
		}
	}

	if mpt.FileData {
		if mpt.firstSectionPrinted {
			fmt.Printf(",")
		}
		printed := false
		fmt.Printf(`"file_meta_data": [%s`, "\n")
		// line is a complete output for one metablob tree
		for _, line := range printBuffer {
			if printed {
				fmt.Printf(",")
			}
			fmt.Printf("%s\n", line)
			printed = true
		}
		fmt.Print("]\n")
		mpt.firstSectionPrinted = true
	}

	return nil
}

// outer print loop for the topology
func (mpt *metaPrintTables) PrintTopology(snapshots restic.Snapshots) {
	if mpt.firstSectionPrinted {
		fmt.Printf(",")
	}
	mpt.firstSectionPrinted = true

	printed := false
	fmt.Printf(`  "snap_meta": [%s`, "\n")

	for _, sn := range snapshots {
		if printed {
			fmt.Printf(",")
		}
		type toPrint struct {
			Snapid   restic.ID  `json:"snapid"`
			NumBlobs int        `json:"numblobs"`
			BlobIDs  restic.IDs `json:"blobids"`
		}

		depMetas := mpt.buildTopology(sn.Tree)
		buf, _ := json.Marshal(toPrint{*sn.ID(), len(depMetas), depMetas})
		fmt.Printf("    %s\n", buf)
		printed = true
	}

	fmt.Printf("  ]\n")
}

// print parent -> child metablob table
func (mpt *metaPrintTables) PrintParentChild() {

	if mpt.firstSectionPrinted {
		fmt.Printf(",")
	}
	mpt.firstSectionPrinted = true

	printed := false
	fmt.Printf(`  "parent_child_map": [%s`, "\n")
	for parent, children := range mpt.parentChild {
		if printed {
			fmt.Printf(",")
		}

		type toPrint struct {
			Parent   restic.ID  `json:"parent"`
			Children restic.IDs `json:"children"`
		}
		buf, _ := json.Marshal(toPrint{parent, children.List()})
		fmt.Printf("    %s\n", buf)
		printed = true
	}

	fmt.Printf("  ]\n")
}

func (mpt *metaPrintTables) PrintCompositeKeyDataTable() {
	// sort composite key by metablob and then by position

	// 3. print
	if mpt.firstSectionPrinted {
		fmt.Printf(",")
	}
	fmt.Printf(`  "datablob_table": [%s`, "\n")

	type toPrint struct {
		CompKey  CompositeKey `json:"composite_key"`
		Contents restic.IDs   `json:"contents"`
	}

	printed := false
	for keyForTable, data := range mpt.contentsMap {
		if printed {
			fmt.Printf(",")
		}
		printed = true

		buf, _ := json.Marshal(toPrint{keyForTable, data})
		fmt.Printf("    %s\n", string(buf))
	}

	fmt.Printf("  ]\n")
	mpt.firstSectionPrinted = true
}
