import zipfile, csv, io, collections, json, datetime

z = zipfile.ZipFile('SEQ_GTFS.zip')
def rd(n): return csv.DictReader(io.TextIOWrapper(z.open(n), 'utf-8-sig'))

routes = {r['route_id']: r for r in rd('routes.txt')}
rail_r = {k for k, v in routes.items() if v['route_type'] == '2'}
trips = {t['trip_id']: t for t in rd('trips.txt') if t['route_id'] in rail_r}
stops = {s['stop_id']: s for s in rd('stops.txt')}

rail_sids = set()
maxtime = '00:00:00'
over24 = 0
rows = 0
trip_stops = collections.defaultdict(list)
for st in rd('stop_times.txt'):
    if st['trip_id'] in trips:
        rail_sids.add(st['stop_id'])
        rows += 1
        if st['departure_time'] > maxtime: maxtime = st['departure_time']
        if st['departure_time'] >= '24:00:00': over24 += 1
        trip_stops[st['trip_id']].append((int(st['stop_sequence']), st['stop_id'], st['arrival_time'], st['departure_time']))

print('rail stop_times rows=%d max departure=%s over-24h rows=%d' % (rows, maxtime, over24))

parents = {}
for sid in rail_sids:
    p = stops[sid]['parent_station']
    parents.setdefault(p, []).append(sid)
print('rail stations=%d platforms=%d' % (len(parents), len(rail_sids)))
print('platforms per station:', collections.Counter(len(v) for v in parents.values()))

names = {p: stops[p]['stop_name'] for p in parents}
# ambiguity probe
def probe(q):
    q = q.lower()
    exact = [n for n in names.values() if n.lower().replace(' station','') == q]
    pre = [n for n in names.values() if n.lower().startswith(q)]
    sub = [n for n in names.values() if q in n.lower()]
    print('  %-14s exact=%s prefix=%s substr=%s' % (q, exact, pre, sub))
for q in ['central','roma','roma street','south','wynnum','springfield','park','airport','bowen','helensvale','indro','fortitude']:
    probe(q)

# stations with same name prefix collisions
print('\ncollision groups (prefix of another):')
ns = sorted(n.replace(' station','') for n in names.values())
for a in ns:
    for b in ns:
        if a != b and b.startswith(a + ' '):
            print('   %r is prefix of %r' % (a, b))

# Central / Roma Street detail
for p, sids in parents.items():
    if 'Central' == stops[p]['stop_name'].replace(' station','') or 'Roma Street' in stops[p]['stop_name']:
        print('\n%s (%s) lat=%s lon=%s platforms:' % (stops[p]['stop_name'], p, stops[p]['stop_lat'], stops[p]['stop_lon']))
        for s in sorted(sids):
            print('   ', s, stops[s]['stop_code'], stops[s]['platform_code'], stops[s]['stop_name'])

# service calendar for rail
cal = {c['service_id']: c for c in rd('calendar.txt')}
rail_svc = sorted({t['service_id'] for t in trips.values()})
print('\nrail services:')
for s in rail_svc:
    c = cal.get(s)
    if c: print('  ', s, ''.join(c[d][0] if c[d]=='1' else '.' for d in ['monday','tuesday','wednesday','thursday','friday','saturday','sunday']), c['start_date'], c['end_date'])
    else: print('  ', s, 'NO CALENDAR ROW')
cd = list(rd('calendar_dates.txt'))
print('calendar_dates rows:', len(cd), 'rail-affecting:', sum(1 for r in cd if r['service_id'] in set(rail_svc)))
print('sample cal dates:', cd[:4])

# trips serving both Central and Roma Street today -> ordering check
cent = [p for p in parents if stops[p]['stop_name'] == 'Central station'][0]
roma = [p for p in parents if stops[p]['stop_name'] == 'Roma Street station'][0]
cent_s, roma_s = set(parents[cent]), set(parents[roma])
both = 0
dirn = collections.Counter()
for tid, sts in trip_stops.items():
    sts.sort()
    ids = [s[1] for s in sts]
    ci = next((i for i, s in enumerate(ids) if s in cent_s), None)
    ri = next((i for i, s in enumerate(ids) if s in roma_s), None)
    if ci is not None and ri is not None:
        both += 1
        dirn['central->roma' if ci < ri else 'roma->central'] += 1
print('\ntrips serving both Central & Roma St: %d  %s' % (both, dict(dirn)))

# rt stop ids not in static
rt = json.load(open('rt.json'))
rtstops = {s['stop_id'] for t in rt['trip_updates'] for s in t['stus']}
miss = rtstops - set(stops)
print('rt stop ids missing from static:', miss)
# are all rt rail stops rail stops?
print('rt stops not in rail set:', len(rtstops - rail_sids), sorted(rtstops - rail_sids)[:10])
