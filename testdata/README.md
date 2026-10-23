# Test data: Translink transit data

The files in this directory are **not** covered by the repository's MIT licence.
They are Translink's South East Queensland transit data, published by the
Department of Transport and Main Roads – Translink Division, State of
Queensland, and licensed under
[Creative Commons Attribution 4.0 International (CC BY 4.0)](https://creativecommons.org/licenses/by/4.0/).
Source: [Translink open data](https://translink.com.au/about-translink/open-data).

Data © Translink. Use of this data does not imply endorsement by Translink or
the State of Queensland.

## Files and changes

| File | Source | Changes |
|---|---|---|
| `static.zip` | SEQ GTFS schedule, <https://gtfsrt.api.translink.com.au/GTFS/SEQ_GTFS.zip>, the edition published 21 Oct 2026 (feed dates 20261022–20261221) | **Modified.** A subset cut by `cmd/mkfixture`: rail routes only; the trips of services `QR 26_27-44634-0001000` and `QR 26_27-44635` with their stop times; the stations they call at and those stations' child stops; the matching `calendar` and `calendar_dates` rows; the routes and shapes those trips use, with shapes thinned to every 4th point (end points kept). `agency.txt` and `feed_info.txt` are unchanged. Field values are verbatim. |
| `stations.csv` | Derived from the same GTFS schedule | **Modified.** An extract of the 154 rail stations (`stop_id`, `stop_name`, `stop_lat`, `stop_lon` from `stops.txt`), renamed to `id,name,lat,lon` and sorted by name. |
| `rt/20261023T000334/tu.pb` | GTFS-Realtime TripUpdates (rail), <https://gtfsrt.api.translink.com.au/api/realtime/SEQ/TripUpdates/Rail> | Unmodified capture, 23 Oct 2026 00:03:34 AEST (`cmd/capture`). |
| `rt/20261023T000334/vp.pb` | GTFS-Realtime VehiclePositions (rail), <https://gtfsrt.api.translink.com.au/api/realtime/SEQ/VehiclePositions/Rail> | Unmodified capture, same instant. |
| `rt/20261023T000334/alerts.pb` | GTFS-Realtime Alerts, <https://gtfsrt.api.translink.com.au/api/realtime/SEQ/Alerts> | Unmodified capture, same instant. |
