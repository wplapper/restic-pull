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
*/
