"""Rank candidate interchange stations by rail trip coverage.

Produces the table in SPEC.md 2.4. Also verifies interchange station names
still resolve (Park Road was renamed Boggo Road).
"""
import zipfile, csv, io, collections

z = zipfile.ZipFile('SEQ_GTFS.zip')
def rd(n): return csv.DictReader(io.TextIOWrapper(z.open(n), 'utf-8-sig'))

rail_routes = {r['route_id'] for r in rd('routes.txt') if r['route_type'] == '2'}
trips = {t['trip_id']: t for t in rd('trips.txt') if t['route_id'] in rail_routes}
stops = {s['stop_id']: s for s in rd('stops.txt')}

trip_stations = collections.defaultdict(set)
for st in rd('stop_times.txt'):
    if st['trip_id'] in trips:
        trip_stations[st['trip_id']].add(stops[st['stop_id']]['parent_station'])

names = {}
for sts in trip_stations.values():
    for p in sts:
        names.setdefault(p, stops[p]['stop_name'])

print('stations matching "Park":', sorted(n for n in names.values() if 'Park' in n))
print('stations matching "Boggo":', sorted(n for n in names.values() if 'Boggo' in n))

calls = collections.Counter()
lines = collections.defaultdict(set)
for tid, sts in trip_stations.items():
    code = trips[tid]['route_id'].split('-')[0]  # 4-letter O-D code, version stripped
    for p in sts:
        calls[p] += 1
        lines[p].add(code)

total = len(trips)
print('\n%-28s %7s %7s %s' % ('station', 'trips', '%', 'route codes'))
for p, c in calls.most_common(12):
    print('%-28s %7d %6.1f%% %d' % (names[p], c, 100 * c / total, len(lines[p])))

CHOSEN = ['Bowen Hills station', 'Roma Street station', 'Central station',
          'Fortitude Valley station', 'Eagle Junction station',
          'Northgate station', 'Boggo Road station']
print('\nchosen interchange set resolves:')
by_name = {v: k for k, v in names.items()}
for n in CHOSEN:
    p = by_name.get(n)
    print('  %-28s %s' % (n, p if p else 'MISSING -- would disable interchange routing'))
