import { Suspense, lazy } from "react";
import { createBrowserRouter } from "react-router";
import type { RouteObject } from "react-router";
import { DeparturesPage } from "../pages/DeparturesPage";
import { NotFoundPage } from "../pages/NotFoundPage";
import { TripPage } from "../pages/TripPage";

const routes: RouteObject[] = [
  { path: "/", Component: DeparturesPage },
  { path: "/trip/:tripId", Component: TripPage },
];

if (import.meta.env.DEV) {
  const Gallery = lazy(() => import("../dev/Gallery").then((m) => ({ default: m.Gallery })));
  routes.push({
    path: "/gallery",
    element: (
      <Suspense>
        <Gallery />
      </Suspense>
    ),
  });
}

routes.push({ path: "*", Component: NotFoundPage });

/** The app's browser router. The gallery route exists only in dev builds. */
export const router = createBrowserRouter(routes);
