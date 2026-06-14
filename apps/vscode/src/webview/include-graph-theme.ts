import type { AspGraphLink } from "../include-graph-webview";
import type { GraphThemePalette, NodeColorCategory, WebviewTheme } from "./include-graph-types";

const darkNodeColors: Record<NodeColorCategory, string> = {
  root: "#ffffff",
  file: "#67d8ef",
  missingInclude: "#ff4db8",
  function: "#c792ea",
  sub: "#b39ddb",
  class: "#c3e88d",
  method: "#a6e3a1",
  methodFunction: "#7ee787",
  methodSub: "#b3f27c",
  property: "#ff9cac",
  member: "#ffb86c",
  globalVariable: "#ffcb6b",
  implicitGlobalVariable: "#f78c6c",
  globalConstant: "#82aaff",
  localVariable: "#dcdcaa",
  localConstant: "#80cbc4",
  parameter: "#b2ccd6",
  unresolvedFunction: "#ff9cac",
  unresolved: "#ff5370",
};

const lightNodeColors: Record<NodeColorCategory, string> = {
  root: "#111827",
  file: "#0369a1",
  missingInclude: "#be185d",
  function: "#7e22ce",
  sub: "#5b21b6",
  class: "#15803d",
  method: "#047857",
  methodFunction: "#15803d",
  methodSub: "#4d7c0f",
  property: "#be123c",
  member: "#b45309",
  globalVariable: "#b45309",
  implicitGlobalVariable: "#c2410c",
  globalConstant: "#1d4ed8",
  localVariable: "#854d0e",
  localConstant: "#0f766e",
  parameter: "#475569",
  unresolvedFunction: "#be123c",
  unresolved: "#dc2626",
};

const darkLinkColors: Record<AspGraphLink["kind"], string> = {
  include: "#82aaff",
  declares: "#89ddff",
  references: "#c3e88d",
  assignments: "#ffcb6b",
  calls: "#f78c6c",
  unresolvedReference: "#ff5370",
};

const lightLinkColors: Record<AspGraphLink["kind"], string> = {
  include: "#2563eb",
  declares: "#0284c7",
  references: "#16a34a",
  assignments: "#ca8a04",
  calls: "#ea580c",
  unresolvedReference: "#dc2626",
};

export const graphThemePalettes: Record<WebviewTheme, GraphThemePalette> = {
  dark: {
    canvasBackground: "#11151c",
    mutedLink: "#29313d",
    mutedNode: "#2d3542",
    nodeColors: darkNodeColors,
    linkFilterColors: {
      ...darkLinkColors,
      member: darkNodeColors.member,
    },
  },
  light: {
    canvasBackground: "#f8fafc",
    mutedLink: "#cbd5e1",
    mutedNode: "#cbd5e1",
    nodeColors: lightNodeColors,
    linkFilterColors: {
      ...lightLinkColors,
      member: lightNodeColors.member,
    },
  },
};
