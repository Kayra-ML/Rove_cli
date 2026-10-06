import React from "react";
import { createRoot } from "react-dom/client";
import { App } from "./app/App";
import { bootPrefs } from "./lib/themes";
import { bootPlatform } from "./lib/platform";
import "./styles/global.css";

bootPrefs();
bootPlatform();

createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
