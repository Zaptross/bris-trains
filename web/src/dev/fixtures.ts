import type { Alert, Call, Change, Journey, Leg, Punctuality, Station } from "../api/types";

/** Fixed "now" for the gallery: Fri 23 Oct 2026, 08:00 AEST. */
export const NOW = new Date("2026-10-23T08:00:00+10:00");

/** ISO time on the fixture day: t("08:04") or t("08:04:34"). */
export function t(hms: string): string {
  return `2026-10-23T${hms.length === 5 ? `${hms}:00` : hms}+10:00`;
}

function st(id: string, name: string, lat: number, lon: number, aliases: string[] = []): Station {
  return { id, name, lat, lon, aliases };
}

/** Real station subset (testdata/stations.csv) with the SPEC §3.7 curated aliases. */
export const stations: Station[] = [
  st("place_aldsta", "Alderley station", -27.423431, 153.000911),
  st("place_parsta", "Boggo Road station", -27.493302, 153.030405, ["park road"]),
  st("place_bowsta", "Bowen Hills station", -27.445713, 153.038071),
  st("place_censta", "Central station", -27.466076, 153.02632),
  st("place_clesta", "Cleveland station", -27.524498, 153.265865),
  st("place_domsta", "Domestic Airport station", -27.384928, 153.119077, ["domestic"]),
  st("place_egjsta", "Eagle Junction station", -27.414958, 153.050255),
  st("place_enosta", "Enoggera station", -27.420349, 152.992402),
  st("place_fersta", "Ferny Grove station", -27.401865, 152.935651),
  st("place_forsta", "Fortitude Valley station", -27.456779, 153.033243, ["fortitude"]),
  st("place_gaysta", "Gaythorne station", -27.41907, 152.984639),
  st("place_indsta", "Indooroopilly station", -27.503013, 152.976083, ["indro"]),
  st("place_intsta", "International Airport station", -27.402924, 153.108816, ["int airport"]),
  st("place_mitsta", "Mitchelton station", -27.411626, 152.976087),
  st("place_newsta", "Newmarket station", -27.43302, 153.010165),
  st("place_oxfsta", "Oxford Park station", -27.404415, 152.96895),
  st("place_romsta", "Roma Street station", -27.465617, 153.019385, ["roma st"]),
  st("place_sbasta", "South Bank station", -27.481157, 153.022756),
  st("place_sousta", "South Brisbane station", -27.474952, 153.018539),
  st("place_spcsta", "Springfield Central station", -27.674125, 152.902793),
  st("place_wilsta", "Wilston station", -27.436211, 153.019034),
  st("place_winsta", "Windsor station", -27.434934, 153.029041),
  st("place_wynsta", "Wynnum Central station", -27.445791, 153.17073),
  st("place_wyhsta", "Wynnum North station", -27.437388, 153.158586),
  st("place_wnmsta", "Wynnum station", -27.43983, 153.165625),
];

function call(p: Partial<Call> & Pick<Call, "stationId" | "name" | "scheduled">): Call {
  return {
    platform: null,
    expected: p.scheduled,
    actual: null,
    position: "upcoming",
    source: "predicted",
    skipped: false,
    ...p,
  };
}

/** Timeline covering every Position × Source × skipped state. Index 4 is current. */
export const timelineCalls: Call[] = [
  call({ stationId: "place_fersta", name: "Ferny Grove station", platform: "1", scheduled: t("07:48"), actual: t("07:48:20"), expected: t("07:48:20"), position: "passed", source: "observed" }),
  call({ stationId: "place_oxfsta", name: "Oxford Park station", platform: "2", scheduled: t("07:51"), actual: t("07:51:34"), expected: t("07:51:34"), position: "passed", source: "observed" }),
  call({ stationId: "place_mitsta", name: "Mitchelton station", platform: "2", scheduled: t("07:53"), position: "passed", source: "scheduled" }),
  call({ stationId: "place_gaysta", name: "Gaythorne station", platform: "2", scheduled: t("07:55"), position: "passed", source: "scheduled", skipped: true }),
  call({ stationId: "place_enosta", name: "Enoggera station", platform: "2", scheduled: t("07:57"), expected: t("07:59"), position: "current", source: "predicted" }),
  call({ stationId: "place_aldsta", name: "Alderley station", platform: "2", scheduled: t("07:59"), expected: t("08:01:10"), source: "predicted" }),
  call({ stationId: "600192", name: "Unknown stop 600192", scheduled: t("08:01"), expected: t("08:03"), source: "predicted" }),
  call({ stationId: "place_wilsta", name: "Wilston station", platform: "2", scheduled: t("08:04"), source: "predicted", skipped: true }),
  call({ stationId: "place_winsta", name: "Windsor station", platform: "2", scheduled: t("08:06"), expected: t("08:08"), source: "predicted" }),
  call({ stationId: "place_bowsta", name: "Bowen Hills station", platform: "4", scheduled: t("08:09"), expected: t("08:11"), source: "projected" }),
  call({ stationId: "place_forsta", name: "Fortitude Valley station", platform: "2", scheduled: t("08:12"), source: "scheduled" }),
];

/** Example alerts. */
export const alerts: Alert[] = [
  {
    id: "QR-1",
    header: "Ferny Grove line: minor delays",
    description: "Trains may be running up to 10 minutes late due to an earlier signal fault near Mitchelton.\nAllow extra travel time.",
    effect: "SIGNIFICANT_DELAYS",
    cause: "TECHNICAL_PROBLEM",
    activeUntil: t("10:00"),
  },
  {
    id: "QR-2",
    header: "Cleveland line: buses replace trains Manly – Cleveland this weekend",
    description: "Sat 24 and Sun 25 Oct, all day. Rail replacement buses depart from the station forecourt.",
    effect: "REDUCED_SERVICE",
    cause: "MAINTENANCE",
    activeUntil: null,
  },
];

/** Builds a minimal leg. */
export function leg(p: Partial<Leg> & Pick<Leg, "tripId" | "calls">): Leg {
  return {
    serviceDate: "2026-10-23",
    runNumber: null,
    routeName: "Ferny Grove – Beenleigh",
    routeColor: "#D53E45",
    headsign: "Beenleigh",
    board: 0,
    alight: p.calls.length - 1,
    punctuality: "onTime",
    delaySeconds: 0,
    vehicle: null,
    alerts: [],
    shapeId: null,
    ...p,
  };
}

/** Ferny Grove trip with the full timeline; vehicle in transit to Enoggera. */
export const timelineLeg: Leg = leg({
  tripId: "34085617-QR 26_27-44635-1776",
  runNumber: "1776",
  calls: timelineCalls,
  board: 0,
  alight: 9,
  punctuality: "late",
  delaySeconds: 120,
  vehicle: {
    lat: -27.4205,
    lon: 152.99,
    atSequence: 5,
    atIndex: 4,
    status: "IN_TRANSIT_TO",
    observedAt: t("07:59:48"),
    stale: false,
  },
  alerts: alerts.slice(0, 1),
});

function cr(
  id: string,
  dep: string,
  delay: number,
  punctuality: Punctuality,
  route: string,
  color: string,
  headsign: string,
  platform: string | null,
  run: string | null,
): Journey {
  const depart = new Date(new Date(t(dep)).getTime() + delay * 1000).toISOString();
  const arrive = new Date(new Date(t(dep)).getTime() + (delay + 180) * 1000).toISOString();
  const l = leg({
    tripId: id,
    runNumber: run,
    routeName: route,
    routeColor: color,
    headsign,
    punctuality,
    delaySeconds: delay,
    calls: [
      call({ stationId: "place_censta", name: "Central station", platform, scheduled: t(dep), expected: depart }),
      call({ stationId: "place_romsta", name: "Roma Street station", platform: "4", scheduled: t(dep), expected: arrive }),
    ],
  });
  return { legs: [l], change: null, depart, arrive, punctuality };
}

/** Central → Roma Street departures covering every punctuality. */
export const centralToRoma: Journey[] = [
  cr("t1", "08:00:10", 0, "onTime", "Ferny Grove – Beenleigh", "#D53E45", "Beenleigh", "2", "1778"),
  cr("t2", "08:04", 34, "onTime", "Redcliffe Peninsula – Springfield", "#0071BC", "Springfield Central", "5", "DK78"),
  cr("t3", "08:09", 95, "late", "Shorncliffe – Cleveland", "#00A8E5", "Cleveland", "2", "1890"),
  cr("t5", "08:16", -75, "early", "Doomben – Brisbane City", "A54399", "Roma Street", "2", "JRA8"),
  cr("t4", "08:13", 420, "veryLate", "Caboolture – Ipswich", "#008752", "Ipswich", "3", "TP42"),
  cr("t6", "08:28", 0, "scheduled", "Caboolture – Ipswich", "#008752", "Ipswich", null, null),
  cr("t7", "08:34", 0, "skipping", "Airport – Gold Coast", "#FFC425", "Varsity Lakes", "5", "AG12"),
  cr("t8", "09:05", 0, "cancelled", "Shorncliffe – Cleveland", "#00A8E5", "Cleveland", "2", "1894"),
];

function twoLeg(dep1: string, arr1: string, dep2: string, arr2: string, change: Change, run1: string, run2: string, p: Punctuality, delay: number): Journey {
  const l1 = leg({
    tripId: `fg-${run1}`,
    runNumber: run1,
    routeName: "Ferny Grove – Beenleigh",
    routeColor: "#D53E45",
    headsign: "Beenleigh",
    punctuality: p,
    delaySeconds: delay,
    calls: [
      call({ stationId: "place_fersta", name: "Ferny Grove station", platform: "1", scheduled: t(dep1) }),
      call({ stationId: "place_bowsta", name: "Bowen Hills station", platform: "4", scheduled: t(arr1) }),
    ],
  });
  const l2 = leg({
    tripId: `cl-${run2}`,
    runNumber: run2,
    routeName: "Shorncliffe – Cleveland",
    routeColor: "#00A8E5",
    headsign: "Cleveland",
    calls: [
      call({ stationId: "place_bowsta", name: "Bowen Hills station", platform: "2", scheduled: t(dep2) }),
      call({ stationId: "place_clesta", name: "Cleveland station", platform: "1", scheduled: t(arr2) }),
    ],
  });
  return { legs: [l1, l2], change, depart: t(dep1), arrive: t(arr2), punctuality: p };
}

/** Ferny Grove → Cleveland via Bowen Hills: one comfortable change, one tight. */
export const fernyToCleveland: Journey[] = [
  twoLeg("08:11", "08:32", "08:39", "09:32", { stationId: "place_bowsta", name: "Bowen Hills station", bufferSeconds: 420, tight: false }, "1780", "1892", "late", 120),
  twoLeg("08:41", "09:02", "09:06", "10:02", { stationId: "place_bowsta", name: "Bowen Hills station", bufferSeconds: 240, tight: true }, "1782", "D894", "onTime", 0),
];

/** Freshness states. */
export const freshness = {
  live: { generatedAt: t("07:59:58"), dataAgeSeconds: 5, degraded: false },
  stale: { generatedAt: t("07:58:00"), dataAgeSeconds: 60, degraded: false },
  degraded: { generatedAt: t("07:59:58"), dataAgeSeconds: 0, degraded: true },
};
