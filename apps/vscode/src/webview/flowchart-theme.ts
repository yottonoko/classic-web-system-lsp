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
