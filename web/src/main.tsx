import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { Gallery } from "./dev/Gallery";
import "./styles/global.css";

const root = document.getElementById("root");
if (!root) throw new Error("missing #root");

createRoot(root).render(
  <StrictMode>{location.pathname === "/gallery" ? <Gallery /> : <p style={{ padding: 16 }}>Trains</p>}</StrictMode>,
);
