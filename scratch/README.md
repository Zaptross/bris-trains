# Research scratch

The probes and analysis scripts behind `SPEC.md`. The code here (`*.py`,
`rtprobe/`) is covered by the repository's MIT licence.

## Translink data

`rt.json` and `rt2.json` are **not** covered by the MIT licence. They are
Translink's South East Queensland GTFS-Realtime rail data, published by the
Department of Transport and Main Roads – Translink Division, State of
Queensland, and licensed under
[Creative Commons Attribution 4.0 International (CC BY 4.0)](https://creativecommons.org/licenses/by/4.0/).
Source: [Translink open data](https://translink.com.au/about-translink/open-data).
Data © Translink. Use of this data does not imply endorsement by Translink or
the State of Queensland.

**Modified.** Each is a snapshot of the TripUpdates/Rail,
VehiclePositions/Rail and Alerts feeds, taken about five minutes apart on the
evening of 22 Oct 2026. `rtprobe/cmd/dump` decoded them from protobuf and
re-encoded a subset of fields as JSON; zero values were dropped in the
process.

The full static GTFS downloads (`*.zip`) are gitignored and not
redistributed.
