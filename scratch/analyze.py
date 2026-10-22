import zipfile, csv, io, collections, sys

z = zipfile.ZipFile('SEQ_GTFS.zip')
def rd(n): return csv.DictReader(io.TextIOWrapper(z.open(n), 'utf-8-sig'))

routes = {r['route_id']: r for r in rd('routes.txt')}
rail_routes = {k: v for k, v in routes.items() if v['route_type'] == '2'}
print('rail route_ids:', len(rail_routes))
print('distinct rail short names:', len({v['route_short_name'] for v in rail_routes.values()}))
print('distinct rail colors:', collections.Counter(v['route_color'] for v in rail_routes.values()))

trips = list(rd('trips.txt'))
print('trips hdr:', list(trips[0].keys()))
rail_trips = [t for t in trips if t['route_id'] in rail_routes]
print('total trips:', len(trips), 'rail trips:', len(rail_trips))
print('sample rail trip:', rail_trips[0])
rail_trip_ids = {t['trip_id'] for t in rail_trips}
print('direction_id counter:', collections.Counter(t['direction_id'] for t in rail_trips))
print('headsign sample:', collections.Counter(t['trip_headsign'] for t in rail_trips).most_common(10))
print('services:', len({t['service_id'] for t in rail_trips}))

stops = {s['stop_id']: s for s in rd('stops.txt')}

# stream stop_times for rail trips only
rail_stop_ids = set()
per_trip = collections.Counter()
hdr = None
first = []
for i, st in enumerate(rd('stop_times.txt')):
    if hdr is None:
        hdr = list(st.keys()); print('stop_times hdr:', hdr)
    if st['trip_id'] in rail_trip_ids:
        rail_stop_ids.add(st['stop_id'])
        per_trip[st['trip_id']] += 1
        if len(first) < 4 and st['trip_id'] == rail_trips[0]['trip_id']:
            first.append(st)
print('rail stop_times rows:', sum(per_trip.values()))
print('rail stops used:', len(rail_stop_ids))
print('stops per trip: min %d max %d' % (min(per_trip.values()), max(per_trip.values())))
print('first trip stop_times:', first)

parents = collections.Counter()
noparent = []
for sid in rail_stop_ids:
    s = stops[sid]
    p = s['parent_station']
    if p:
        parents[p] += 1
    else:
        noparent.append((sid, s['stop_name']))
print('rail parent stations:', len(parents))
print('rail stops without parent:', len(noparent), noparent[:10])

names = sorted({stops[p]['stop_name'] for p in parents if p in stops})
print('parent station count resolvable:', len(names))
print('ALL RAIL STATION NAMES:')
for n in names:
    print('  ', n)
