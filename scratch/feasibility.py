"""Prove the core query is answerable: next 5 Central -> Roma Street with realtime overlay."""
import zipfile, csv, io, json, collections, datetime, bisect

TZ = datetime.timezone(datetime.timedelta(hours=10))  # Australia/Brisbane, no DST
z = zipfile.ZipFile('SEQ_GTFS.zip')
def rd(n): return csv.DictReader(io.TextIOWrapper(z.open(n), 'utf-8-sig'))

routes = {r['route_id']: r for r in rd('routes.txt')}
rail_r = {k for k, v in routes.items() if v['route_type'] == '2'}
trips = {t['trip_id']: t for t in rd('trips.txt') if t['route_id'] in rail_r}
stops = {s['stop_id']: s for s in rd('stops.txt')}

cal = {c['service_id']: c for c in rd('calendar.txt')}
caldates = collections.defaultdict(dict)
for r in rd('calendar_dates.txt'):
    caldates[r['service_id']][r['date']] = r['exception_type']

DAYS = ['monday','tuesday','wednesday','thursday','friday','saturday','sunday']
def active(sid, d: datetime.date):
    ds = d.strftime('%Y%m%d')
    ex = caldates.get(sid, {}).get(ds)
    if ex == '1': return True
    if ex == '2': return False
    c = cal.get(sid)
    if not c: return False
    return c['start_date'] <= ds <= c['end_date'] and c[DAYS[d.weekday()]] == '1'

def hhmmss(s):
    h, m, sec = (int(x) for x in s.split(':'))
    return h*3600 + m*60 + sec

trip_stops = collections.defaultdict(list)
for st in rd('stop_times.txt'):
    if st['trip_id'] in trips:
        trip_stops[st['trip_id']].append(
            (int(st['stop_sequence']), st['stop_id'], hhmmss(st['arrival_time']), hhmmss(st['departure_time'])))
for v in trip_stops.values():
    v.sort()

rt = json.load(open('rt.json'))
now = datetime.datetime.fromtimestamp(rt['header_ts'], TZ)
print('feed ts:', now.isoformat())

tu_by_trip = {t['trip_id']: t for t in rt['trip_updates']}
vp_by_trip = {v['trip_id']: v for v in rt['vehicles']}

def station(name):
    pid = next(p for p, s in stops.items() if s['stop_name'] == name)
    return pid, {sid for sid, s in stops.items() if s['parent_station'] == pid}

ORIG, DEST = 'Central station', 'Roma Street station'
opid, oset = station(ORIG)
dpid, dset = station(DEST)

# candidate service days: today and yesterday (for post-midnight trips)
today = now.date()
cands = []
for day_off in (-1, 0, 1):
    d = today + datetime.timedelta(days=day_off)
    svc = {s for s in {t['service_id'] for t in trips.values()} if active(s, d)}
    if not svc: continue
    midnight = datetime.datetime.combine(d, datetime.time(0, 0), TZ)
    for tid, t in trips.items():
        if t['service_id'] not in svc: continue
        sts = trip_stops.get(tid)
        if not sts: continue
        oi = next((i for i, s in enumerate(sts) if s[1] in oset), None)
        if oi is None: continue
        di = next((i for i, s in enumerate(sts) if s[1] in dset and i > oi), None)
        if di is None: continue
        dep = midnight + datetime.timedelta(seconds=sts[oi][3])
        arr = midnight + datetime.timedelta(seconds=sts[di][2])
        cands.append((dep, arr, tid, d, sts[oi], sts[di]))

cands.sort()
upcoming = [c for c in cands if c[0] >= now - datetime.timedelta(minutes=2)][:14]
print('\nscheduled candidates %s -> %s (next 14):' % (ORIG, DEST))

shown = 0
for dep, arr, tid, d, ost, dst in upcoming:
    t = trips[tid]
    r = routes[t['route_id']]
    tu = tu_by_trip.get(tid)
    status, rtdep, plat, vehpos = 'no realtime', None, stops[ost[1]]['platform_code'], None
    if tu:
        stu = next((s for s in tu['stus'] if s['seq'] == ost[0]), None)
        if stu:
            delay = stu.get('dep_delay', 0)
            rtdep = datetime.datetime.fromtimestamp(stu.get('dep_time') or stu.get('arr_time'), TZ)
            status = 'ON TIME' if abs(delay) < 60 else ('LATE %ds' % delay if delay > 0 else 'EARLY %ds' % -delay)
            if stu.get('assigned_stop_id'):
                plat = stops.get(stu['assigned_stop_id'], {}).get('platform_code', plat)
            if stu['stu_rel'] == 'SKIPPED':
                status = 'SKIPPING ' + ORIG
        status += ' [%s]' % tu['trip_rel']
    vp = vp_by_trip.get(tid)
    if vp:
        vehpos = '%s @%.4f,%.4f seq%d %s' % (vp['label'], vp['lat'], vp['lon'], vp['cur_seq'], vp['status'])
    print('  %s  sched %s  rt %s  plat %-3s  %-28s  %s  %s' % (
        tid.split('-')[-1], dep.strftime('%H:%M'),
        rtdep.strftime('%H:%M') if rtdep else '  -  ', plat,
        r['route_long_name'] + ' (' + t['trip_headsign'] + ')', status, vehpos or ''))
    shown += 1

# Timeline feasibility for one realtime trip
print('\n--- timeline for a live trip ---')
live = next((t for t in rt['trip_updates'] if t['trip_id'] in vp_by_trip), None)
vp = vp_by_trip[live['trip_id']]
print('trip %s route %s  vehicle %s  status %s seq %s' % (live['trip_id'], live['route_id'], vp['label'], vp['status'], vp['cur_seq']))
for s in live['stus']:
    nm = stops.get(s['stop_id'], {}).get('stop_name', '?' + s['stop_id'])
    tsec = s.get('dep_time') or s.get('arr_time')
    ts = datetime.datetime.fromtimestamp(tsec, TZ).strftime('%H:%M:%S') if tsec else '--:--:--'
    off = (tsec - rt['header_ts']) / 60 if tsec else 0
    mark = '>>' if s['seq'] == vp['cur_seq'] else ('  ' if off > 0 else ' *')
    print('  %s seq%-3d %-32s %s  %+6.1f min  delay %+4ds %s' % (
        mark, s['seq'], nm, ts, off, s.get('dep_delay', s.get('arr_delay', 0)), s['stu_rel'] if s['stu_rel'] != 'SCHEDULED' else ''))
