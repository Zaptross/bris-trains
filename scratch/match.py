import zipfile, csv, io, json, collections, datetime

rt = json.load(open('rt.json'))
tus = rt['trip_updates']
vps = rt['vehicles']
rt_trips = {t['trip_id'] for t in tus}
rt_routes = {t['route_id'] for t in tus}
rt_stops = set()
for t in tus:
    for s in t['stus']:
        rt_stops.add(s['stop_id'])
        if s.get('assigned_stop_id'):
            rt_stops.add(s['assigned_stop_id'])

print('rt trips=%d routes=%d stops=%d vehicles=%d' % (len(rt_trips), len(rt_routes), len(rt_stops), len(vps)))

for zipname in ('SEQ_GTFS.zip', 'SEQ_SCH_GTFS.zip'):
    z = zipfile.ZipFile(zipname)
    def rd(n): return csv.DictReader(io.TextIOWrapper(z.open(n), 'utf-8-sig'))
    st_trips = {t['trip_id'] for t in rd('trips.txt')}
    st_routes = {r['route_id'] for r in rd('routes.txt')}
    st_stops = {s['stop_id'] for s in rd('stops.txt')}
    print('\n== %s ==' % zipname)
    print('  static trips=%d routes=%d stops=%d' % (len(st_trips), len(st_routes), len(st_stops)))
    print('  rt trip_id found in static: %d/%d' % (len(rt_trips & st_trips), len(rt_trips)))
    print('  rt route_id found in static: %d/%d' % (len(rt_routes & st_routes), len(rt_routes)))
    print('  rt stop_id found in static: %d/%d' % (len(rt_stops & st_stops), len(rt_stops)))
    missing_r = sorted(rt_routes - st_routes)[:8]
    print('  missing routes sample:', missing_r)
    # dataset-version token distribution
    ver = collections.Counter(t.split('-')[1] if len(t.split('-')) > 2 else '?' for t in st_trips)
    print('  static trip_id version tokens:', ver.most_common(5))

ver_rt = collections.Counter(t.split('-')[1] for t in rt_trips)
print('\nrt trip_id version tokens:', ver_rt.most_common(5))
print('rt route suffix tokens:', collections.Counter(r.split('-')[1] for r in rt_routes).most_common(8))

# rel breakdown + is a REPLACEMENT trip id in static?
z = zipfile.ZipFile('SEQ_GTFS.zip')
st_trips = {t['trip_id']: t for t in csv.DictReader(io.TextIOWrapper(z.open('trips.txt'), 'utf-8-sig'))}
byrel = collections.defaultdict(lambda: [0, 0])
for t in tus:
    byrel[t['trip_rel']][0] += 1
    if t['trip_id'] in st_trips:
        byrel[t['trip_rel']][1] += 1
print('\nper trip_rel (count, matched-in-static):', dict(byrel))

# direction_id present?
print('direction_id present on TU:', sum(1 for t in tus if t.get('direction_id') is not None))
# does TU include route_id always
print('route_id present on TU:', sum(1 for t in tus if t['route_id']))
# assigned_stop_id differs from stop_id?
diff = sum(1 for t in tus for s in t['stus'] if s.get('assigned_stop_id') and s['assigned_stop_id'] != s['stop_id'])
tot = sum(1 for t in tus for s in t['stus'] if s.get('assigned_stop_id'))
print('assigned_stop_id present=%d differs=%d' % (tot, diff))

# past vs future STUs: how far back does the feed keep a trip
now = rt['header_ts']
spans = []
for t in tus:
    times = [s.get('dep_time') or s.get('arr_time') for s in t['stus'] if (s.get('dep_time') or s.get('arr_time'))]
    if times:
        spans.append((min(times) - now, max(times) - now))
spans.sort()
print('\nTU time spans rel to feed ts (minutes): earliest start %.0f, latest end %.0f' % (min(s[0] for s in spans)/60, max(s[1] for s in spans)/60))
fut = sorted(s[0] for s in spans)
print('trip start offsets (min), first 10:', [round(x/60) for x in fut[:10]])
print('trip start offsets (min), last 10:', [round(x/60) for x in fut[-10:]])

# vehicle coverage
vt = {v['trip_id'] for v in vps}
print('\nTU trips with a vehicle position: %d/%d' % (len(vt & rt_trips), len(rt_trips)))
print('vehicle label sample:', [ (v['label'], v['vehicle_id'][:8], v['status'], v['cur_seq']) for v in vps[:5] ])

# does trip_id suffix == vehicle label?
same = sum(1 for v in vps if v['trip_id'].rsplit('-', 1)[-1] == v['label'])
print('trip_id suffix == vehicle label: %d/%d' % (same, len(vps)))

# alerts touching rail routes
z2 = zipfile.ZipFile('SEQ_GTFS.zip')
rail_rids = {r['route_id'] for r in csv.DictReader(io.TextIOWrapper(z2.open('routes.txt'), 'utf-8-sig')) if r['route_type'] == '2'}
ra = [a for a in rt['alerts'] if set(a['routes'] or []) & rail_rids or set(a['trips'] or []) & rt_trips]
print('\nalerts touching rail: %d / %d' % (len(ra), len(rt['alerts'])))
for a in ra[:3]:
    print('  effect=%s cause=%s routes=%d stops=%d header=%r' % (a['effect'], a['cause'], len(a['routes'] or []), len(a['stops'] or []), (a['header'] or '')[:110]))
print('alert effects on rail:', collections.Counter(a['effect'] for a in ra))
print('alerts with stop-only informed entity:', sum(1 for a in rt['alerts'] if a['stops'] and not a['routes']))
print('alert sample desc:', repr((ra[0]['desc'] or '')[:300]) if ra else None)

# SKIPPED stops
sk = [(t['trip_id'], s['stop_id']) for t in tus for s in t['stus'] if s['stu_rel'] == 'SKIPPED']
print('\nSKIPPED stop_time_updates:', len(sk), sk[:3])
