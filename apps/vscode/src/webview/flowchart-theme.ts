import type {
  WebviewTheme,
  FlowchartNodeKind,
  FlowchartNodeLinkRole,
  FlowchartThemePalette,
  FlowchartVisualStyle,
} from "./flowchart-types";

const darkFlowchartNodeKindStyles: Record<FlowchartNodeKind, FlowchartVisualStyle> = {
  start: {
    background: "#132538",
    border: "#7dd3fc",
    mermaidClass: "flowStart",
    text: "#e0f2fe",
  },
  end: {
    background: "#1f2937",
    border: "#94a3b8",
    mermaidClass: "flowEnd",
    text: "#f1f5f9",
  },
  if: {
    background: "#2f2410",
    border: "#f6c177",
    mermaidClass: "flowBranch",
    text: "#ffe8b6",
  },
  elseif: {
    background: "#2f2410",
    border: "#f6c177",
    mermaidClass: "flowBranch",
    text: "#ffe8b6",
  },
  else: {
    background: "#2f2410",
    border: "#f6c177",
    mermaidClass: "flowBranch",
    text: "#ffe8b6",
  },
  select: {
    background: "#2f2410",
    border: "#f6c177",
    mermaidClass: "flowBranch",
    text: "#ffe8b6",
  },
  case: {
    background: "#2f2410",
    border: "#f6c177",
    mermaidClass: "flowBranch",
    text: "#ffe8b6",
  },
  for: {
    background: "#251b35",
    border: "#c792ea",
    mermaidClass: "flowLoop",
    text: "#ead7ff",
  },
  forEach: {
    background: "#251b35",
    border: "#c792ea",
    mermaidClass: "flowLoop",
    text: "#ead7ff",
  },
  do: {
    background: "#251b35",
    border: "#c792ea",
    mermaidClass: "flowLoop",
    text: "#ead7ff",
  },
  while: {
    background: "#251b35",
    border: "#c792ea",
    mermaidClass: "flowLoop",
    text: "#ead7ff",
  },
  call: {
    background: "#102a2a",
    border: "#63e6be",
    mermaidClass: "flowCall",
    text: "#c8fff1",
  },
  declaration: {
    background: "#302610",
    border: "#ffcb6b",
    mermaidClass: "flowDeclaration",
    text: "#fff0b8",
  },
  exceptionHandling: {
    background: "#321627",
    border: "#ff6fb1",
    mermaidClass: "flowExceptionHandling",
    text: "#ffd6e8",
  },
  exit: {
    background: "#34191d",
    border: "#ff7b8a",
    mermaidClass: "flowExit",
    text: "#ffd4da",
  },
  merge: {
    background: "#1f2937",
    border: "#94a3b8",
    mermaidClass: "flowMerge",
    text: "#f1f5f9",
  },
  output: {
    background: "#102a2a",
    border: "#7ee787",
    mermaidClass: "flowOutput",
    text: "#c8fff1",
  },
  statement: {
    background: "#172131",
    border: "#89ddff",
    mermaidClass: "flowStatement",
    text: "#d9e0ea",
  },
};

const lightFlowchartNodeKindStyles: Record<FlowchartNodeKind, FlowchartVisualStyle> = {
  start: {
    background: "#e0f2fe",
    border: "#0284c7",
    mermaidClass: "flowStart",
    text: "#0f172a",
  },
  end: {
    background: "#e2e8f0",
    border: "#64748b",
    mermaidClass: "flowEnd",
    text: "#0f172a",
  },
  if: {
    background: "#fef3c7",
    border: "#b45309",
    mermaidClass: "flowBranch",
    text: "#3f2a04",
  },
  elseif: {
    background: "#fef3c7",
    border: "#b45309",
    mermaidClass: "flowBranch",
    text: "#3f2a04",
  },
  else: {
    background: "#fef3c7",
    border: "#b45309",
    mermaidClass: "flowBranch",
    text: "#3f2a04",
  },
  select: {
    background: "#fef3c7",
    border: "#b45309",
    mermaidClass: "flowBranch",
    text: "#3f2a04",
  },
  case: {
    background: "#fef3c7",
    border: "#b45309",
    mermaidClass: "flowBranch",
    text: "#3f2a04",
  },
  for: {
    background: "#f3e8ff",
    border: "#7e22ce",
    mermaidClass: "flowLoop",
    text: "#3b0764",
  },
  forEach: {
    background: "#f3e8ff",
    border: "#7e22ce",
    mermaidClass: "flowLoop",
    text: "#3b0764",
  },
  do: {
    background: "#f3e8ff",
    border: "#7e22ce",
    mermaidClass: "flowLoop",
    text: "#3b0764",
  },
  while: {
    background: "#f3e8ff",
    border: "#7e22ce",
    mermaidClass: "flowLoop",
    text: "#3b0764",
  },
  call: {
    background: "#ccfbf1",
    border: "#0f766e",
    mermaidClass: "flowCall",
    text: "#134e4a",
  },
  declaration: {
    background: "#fef3c7",
    border: "#ca8a04",
    mermaidClass: "flowDeclaration",
    text: "#3f2a04",
  },
  exceptionHandling: {
    background: "#fce7f3",
    border: "#db2777",
    mermaidClass: "flowExceptionHandling",
    text: "#831843",
  },
  exit: {
    background: "#ffe4e6",
    border: "#e11d48",
    mermaidClass: "flowExit",
    text: "#881337",
  },
  merge: {
    background: "#e2e8f0",
    border: "#64748b",
    mermaidClass: "flowMerge",
    text: "#0f172a",
  },
  output: {
    background: "#dcfce7",
    border: "#16a34a",
    mermaidClass: "flowOutput",
    text: "#14532d",
  },
  statement: {
    background: "#e0f2fe",
    border: "#0284c7",
    mermaidClass: "flowStatement",
    text: "#0f172a",
  },
};

const darkFlowchartLinkRoleStyles: Record<FlowchartNodeLinkRole, FlowchartVisualStyle> = {
  read: {
    background: "#162816",
    border: "#c3e88d",
    mermaidClass: "flowLinkRead",
    text: "#e5ffd0",
  },
  write: {
    background: "#302610",
    border: "#ffcb6b",
    mermaidClass: "flowLinkWrite",
    text: "#fff0b8",
  },
  call: {
    background: "#2f1f16",
    border: "#f78c6c",
    mermaidClass: "flowLinkCall",
    text: "#ffd8ca",
  },
  new: {
    background: "#251b35",
    border: "#c792ea",
    mermaidClass: "flowLinkNew",
    text: "#ead7ff",
  },
  member: {
    background: "#2f2314",
    border: "#ffb86c",
    mermaidClass: "flowLinkMember",
    text: "#ffe3bd",
  },
  definition: {
    background: "#112839",
    border: "#89ddff",
    mermaidClass: "flowLinkDefinition",
    text: "#d5f7ff",
  },
  unknown: {
    background: "#1f2937",
    border: "#b2ccd6",
    mermaidClass: "flowLinkUnknown",
    text: "#e4eef3",
  },
};

const lightFlowchartLinkRoleStyles: Record<FlowchartNodeLinkRole, FlowchartVisualStyle> = {
  read: {
    background: "#dcfce7",
    border: "#16a34a",
    mermaidClass: "flowLinkRead",
    text: "#14532d",
  },
  write: {
    background: "#fef3c7",
    border: "#ca8a04",
    mermaidClass: "flowLinkWrite",
    text: "#3f2a04",
  },
  call: {
    background: "#ffedd5",
    border: "#ea580c",
    mermaidClass: "flowLinkCall",
    text: "#7c2d12",
  },
  new: {
    background: "#f3e8ff",
    border: "#7e22ce",
    mermaidClass: "flowLinkNew",
    text: "#3b0764",
  },
  member: {
    background: "#ffedd5",
    border: "#c2410c",
    mermaidClass: "flowLinkMember",
    text: "#7c2d12",
  },
  definition: {
    background: "#e0f2fe",
    border: "#0284c7",
    mermaidClass: "flowLinkDefinition",
    text: "#0c4a6e",
  },
  unknown: {
    background: "#e2e8f0",
    border: "#64748b",
    mermaidClass: "flowLinkUnknown",
    text: "#334155",
  },
};

const darkFlowchartSymbolKindStyles: Record<string, FlowchartVisualStyle> = {
  function: {
    background: "#102a2a",
    border: "#63e6be",
    mermaidClass: "flowSymbolFunction",
    text: "#c8fff1",
  },
  sub: {
    background: "#102a2a",
    border: "#7dd3fc",
    mermaidClass: "flowSymbolSub",
    text: "#dff6ff",
  },
  class: {
    background: "#1f2c14",
    border: "#c3e88d",
    mermaidClass: "flowSymbolClass",
    text: "#e5ffd0",
  },
  method: {
    background: "#2f1f16",
    border: "#f78c6c",
    mermaidClass: "flowSymbolMethod",
    text: "#ffd8ca",
  },
  property: {
    background: "#351c28",
    border: "#ff9cac",
    mermaidClass: "flowSymbolProperty",
    text: "#ffdce8",
  },
  variable: {
    background: "#302610",
    border: "#ffcb6b",
    mermaidClass: "flowSymbolVariable",
    text: "#fff0b8",
  },
  implicitglobalvariable: {
    background: "#332014",
    border: "#f78c6c",
    mermaidClass: "flowSymbolImplicitGlobalVariable",
    text: "#ffe0cf",
  },
  unresolvedfunction: {
    background: "#351c28",
    border: "#ff9cac",
    mermaidClass: "flowSymbolUnresolvedFunction",
    text: "#ffdce8",
  },
  constant: {
    background: "#16243a",
    border: "#82aaff",
    mermaidClass: "flowSymbolConstant",
    text: "#dce8ff",
  },
  parameter: {
    background: "#1b2930",
    border: "#b2ccd6",
    mermaidClass: "flowSymbolParameter",
    text: "#e4eef3",
  },
  field: {
    background: "#2f2314",
    border: "#ffb86c",
    mermaidClass: "flowSymbolField",
    text: "#ffe3bd",
  },
  member: {
    background: "#2f2314",
    border: "#ffb86c",
    mermaidClass: "flowSymbolMember",
    text: "#ffe3bd",
  },
};

const lightFlowchartSymbolKindStyles: Record<string, FlowchartVisualStyle> = {
  function: {
    background: "#ccfbf1",
    border: "#0f766e",
    mermaidClass: "flowSymbolFunction",
    text: "#134e4a",
  },
  sub: {
    background: "#e0f2fe",
    border: "#0284c7",
    mermaidClass: "flowSymbolSub",
    text: "#0c4a6e",
  },
  class: {
    background: "#dcfce7",
    border: "#16a34a",
    mermaidClass: "flowSymbolClass",
    text: "#14532d",
  },
  method: {
    background: "#ffedd5",
    border: "#ea580c",
    mermaidClass: "flowSymbolMethod",
    text: "#7c2d12",
  },
  property: {
    background: "#ffe4e6",
    border: "#e11d48",
    mermaidClass: "flowSymbolProperty",
    text: "#881337",
  },
  variable: {
    background: "#fef3c7",
    border: "#ca8a04",
    mermaidClass: "flowSymbolVariable",
    text: "#3f2a04",
  },
  implicitglobalvariable: {
    background: "#ffedd5",
    border: "#c2410c",
    mermaidClass: "flowSymbolImplicitGlobalVariable",
    text: "#7c2d12",
  },
  unresolvedfunction: {
    background: "#ffe4e6",
    border: "#e11d48",
    mermaidClass: "flowSymbolUnresolvedFunction",
    text: "#881337",
  },
  constant: {
    background: "#dbeafe",
    border: "#2563eb",
    mermaidClass: "flowSymbolConstant",
    text: "#1e3a8a",
  },
  parameter: {
    background: "#e2e8f0",
    border: "#64748b",
    mermaidClass: "flowSymbolParameter",
    text: "#334155",
  },
  field: {
    background: "#ffedd5",
    border: "#c2410c",
    mermaidClass: "flowSymbolField",
    text: "#7c2d12",
  },
  member: {
    background: "#ffedd5",
    border: "#c2410c",
    mermaidClass: "flowSymbolMember",
    text: "#7c2d12",
  },
};

export const flowchartThemePalettes: Record<WebviewTheme, FlowchartThemePalette> = {
  dark: {
    mermaidTheme: "dark",
    nodeKindStyles: darkFlowchartNodeKindStyles,
    linkRoleStyles: darkFlowchartLinkRoleStyles,
    symbolKindStyles: darkFlowchartSymbolKindStyles,
  },
  light: {
    mermaidTheme: "base",
    mermaidThemeVariables: {
      background: "#f8fafc",
      mainBkg: "#ffffff",
      primaryColor: "#e0f2fe",
      primaryBorderColor: "#0284c7",
      primaryTextColor: "#0f172a",
      lineColor: "#64748b",
      textColor: "#0f172a",
      edgeLabelBackground: "#ffffff",
    },
    nodeKindStyles: lightFlowchartNodeKindStyles,
    linkRoleStyles: lightFlowchartLinkRoleStyles,
    symbolKindStyles: lightFlowchartSymbolKindStyles,
  },
};

/** VS Code chart colors that carry each node group's meaning under the auto theme. */
const flowchartNodeChartColors: Record<string, string> = {
  flowStart: "charts-blue",
  flowStatement: "charts-blue",
  flowEnd: "charts-foreground",
  flowMerge: "charts-foreground",
  flowBranch: "charts-yellow",
  flowLoop: "charts-purple",
  flowCall: "charts-green",
  flowOutput: "charts-green",
  flowDeclaration: "charts-orange",
  flowExceptionHandling: "charts-red",
  flowExit: "charts-red",
};

export type VsCodeColorLookup = (name: string) => string | undefined;

function mermaidSafeColor(value: string): string {
  const match = value.match(
    /^rgba?\(\s*([\d.]+%?)\s*,\s*([\d.]+%?)\s*,\s*([\d.]+%?)(?:\s*,\s*([\d.]+%?))?\s*\)$/i,
  );
  if (!match) {
    return value;
  }
  const byte = (component: string): number => {
    const percentage = component.endsWith("%");
    const numeric = Number.parseFloat(component);
    return Math.round(Math.min(255, Math.max(0, percentage ? (numeric / 100) * 255 : numeric)));
  };
  const alpha = (component: string | undefined): number => {
    if (!component) {
      return 255;
    }
    const percentage = component.endsWith("%");
    const numeric = Number.parseFloat(component);
    return Math.round(
      Math.min(255, Math.max(0, percentage ? (numeric / 100) * 255 : numeric * 255)),
    );
  };
  const hex = [byte(match[1]), byte(match[2]), byte(match[3])]
    .map((component) => component.toString(16).padStart(2, "0"))
    .join("");
  const alphaByte = alpha(match[4]);
  return `#${hex}${alphaByte < 255 ? alphaByte.toString(16).padStart(2, "0") : ""}`;
}

/** Resolves the auto theme to literal VS Code colors so Mermaid SVG exports stay self-contained. */
export function flowchartThemePaletteForSetting(
  theme: WebviewTheme,
  setting: WebviewTheme | "auto" | undefined,
  color: VsCodeColorLookup,
): FlowchartThemePalette {
  const fallback = flowchartThemePalettes[theme];
  if (setting === "light" || setting === "dark") {
    return fallback;
  }
  const resolveColor = (name: string, fallbackColor: string): string =>
    mermaidSafeColor(color(name) ?? fallbackColor);
  const foreground = resolveColor("editor-foreground", theme === "light" ? "#0f172a" : "#d9e0ea");
  const background = resolveColor("editor-background", theme === "light" ? "#ffffff" : "#0d1117");
  const surface = resolveColor("editorWidget-background", background);
  const chart = (name: string): string | undefined => {
    const value = color(name);
    return value ? mermaidSafeColor(value) : undefined;
  };
  const palette = [
    "charts-blue",
    "charts-yellow",
    "charts-purple",
    "charts-green",
    "charts-orange",
    "charts-red",
  ]
    .map(chart)
    .filter((value): value is string => Boolean(value));
  const accent = mermaidSafeColor(
    color("focusBorder") ?? color("textLink-foreground") ?? fallback.nodeKindStyles.start.border,
  );
  // Kinds that share a fixed-theme color (If/ElseIf/Else, every loop, ...) must
  // keep sharing one VS Code color, so colors follow the fixed palette's groups
  // rather than each kind's position in the table.
  const recolor = <T extends string>(
    styles: Record<T, FlowchartVisualStyle>,
    semantic: Record<string, string> = {},
  ): Record<T, FlowchartVisualStyle> => {
    const groupColors = new Map<string, string>();
    return Object.fromEntries(
      Object.entries<FlowchartVisualStyle>(styles).map(([key, style]) => {
        const semanticColor = semantic[style.mermaidClass]
          ? chart(semantic[style.mermaidClass])
          : undefined;
        let border = semanticColor ?? groupColors.get(style.border);
        if (!border) {
          border = palette[groupColors.size % Math.max(palette.length, 1)] ?? accent;
          groupColors.set(style.border, border);
        }
        return [key, { ...style, background: surface, border, text: foreground }];
      }),
    ) as Record<T, FlowchartVisualStyle>;
  };
  return {
    mermaidTheme: "base",
    mermaidThemeVariables: {
      background,
      mainBkg: surface,
      primaryColor: surface,
      primaryBorderColor: accent,
      primaryTextColor: foreground,
      lineColor: resolveColor("charts-foreground", foreground),
      textColor: foreground,
      edgeLabelBackground: surface,
    },
    nodeKindStyles: recolor(fallback.nodeKindStyles, flowchartNodeChartColors),
    linkRoleStyles: recolor(fallback.linkRoleStyles),
    symbolKindStyles: recolor(fallback.symbolKindStyles),
  };
}
