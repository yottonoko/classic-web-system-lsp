export const semanticTokenTypes = [
  "keyword",
  "variable",
  "parameter",
  "function",
  "class",
  "method",
  "property",
  "comment",
  "string",
  "operator",
  "namespace",
  "interface",
  "enum",
  "enumMember",
  "typeAlias",
  "typeParameter",
  "constant",
] as const;

export const semanticTokenModifiers = [
  "public",
  "private",
  "readonly",
  "library",
  "byref",
  "byval",
] as const;

export const semanticTokenTypeIndexes = new Map<string, number>(
  semanticTokenTypes.map((tokenType, index) => [tokenType, index]),
);

export const semanticTokenModifierBitsets = new Map<string, number>(
  semanticTokenModifiers.map((modifier, index) => [modifier, 1 << index]),
);
