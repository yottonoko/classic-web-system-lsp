import type { LogEntry } from "./log-analysis-model";
export interface LogExecution {
  id: number;
  event: string;
  start?: LogEntry;
  end?: LogEntry;
  begin?: number;
  finish?: number;
  estimated: boolean;
  orderedWithoutTime?: boolean;
  parent?: number;
  inferredParent: boolean;
  ambiguous: boolean;
  lane: number;
  top: number;
  bottom: number;
}
function stem(entry: LogEntry): string {
  return entry.event.replace(
    /\.(started|start|received|completed|complete|cancelled|failed|stale|hit|miss)$/,
    "",
  );
}
function text(value: unknown): string {
  return typeof value === "string" || typeof value === "number" ? String(value) : "";
}
function identity(entry: LogEntry): string {
  const kind = stem(entry);
  const spanId = text(entry.fields.spanId);
  if (spanId) return JSON.stringify(["span", text(entry.fields.traceId), spanId, kind]);
  return JSON.stringify([
    kind,
    text(entry.fields.requestId),
    text(entry.fields.method),
    kind.startsWith("lsp.") ? "" : text(entry.fields.uri),
  ]);
}
function rank(event: string): number {
  if (/^lsp\.(request|notification)$/.test(event)) return 0;
  if (event === "analysis.total") return 1;
  if (event === "check.total") return 2;
  if (event === "check.diagnostics") return 3;
  return 4;
}
/** Match only unique lifecycle identities; inferred containment is explicitly marked. */
export function buildLogExecutionTree(entries: LogEntry[], limit = 100) {
  const spans: LogExecution[] = [];
  const active = new Map<string, LogExecution[]>();
  for (const entry of entries) {
    const key = identity(entry);
    const begins = /\.(started|start|received)$/.test(entry.event);
    const ends =
      /\.(completed|complete|cancelled|failed)$/.test(entry.event) || entry.duration !== undefined;
    if (begins) {
      const span: LogExecution = {
        id: entry.line,
        event: stem(entry),
        start: entry,
        begin: entry.timestamp,
        estimated: false,
        inferredParent: false,
        ambiguous: false,
        lane: 0,
        top: 0,
        bottom: 0,
      };
      const pending = active.get(key) ?? [];
      if (pending.length) {
        span.ambiguous = true;
        for (const item of pending) item.ambiguous = true;
      }
      pending.push(span);
      active.set(key, pending);
      spans.push(span);
    } else {
      const pending = active.get(key);
      // Repeated identities without correlation cannot establish which start ended.
      const match = ends && pending?.length === 1 && !pending[0].ambiguous ? pending[0] : undefined;
      if (match) {
        match.end = entry;
        match.finish = entry.timestamp;
        active.delete(key);
      } else
        spans.push({
          id: entry.line,
          event: stem(entry),
          end: entry,
          begin:
            entry.timestamp !== undefined && entry.duration !== undefined
              ? entry.timestamp - entry.duration
              : entry.timestamp,
          finish: entry.timestamp,
          estimated: entry.duration !== undefined,
          inferredParent: false,
          ambiguous: !!pending?.length,
          lane: 0,
          top: 0,
          bottom: 0,
        });
    }
  }
  const positions = new Map(entries.map((entry, index) => [entry.line, index]));
  const anchors = entries.filter((entry) => entry.timestamp !== undefined);
  const monotonic = anchors.every(
    (entry, index) => index === 0 || entry.timestamp! >= anchors[index - 1].timestamp!,
  );
  // Recorded nodes follow file order, even when clocks repeat or move backward.
  // Only an unrecorded start may be interpolated from duration and clock anchors.
  const beginPosition = (span: LogExecution): number => {
    const own = positions.get((span.start ?? span.end!).line)!;
    if (span.start || span.begin === undefined || !span.estimated || !monotonic) return own;
    let low = 0,
      high = anchors.length;
    while (low < high) {
      const middle = (low + high) >>> 1;
      if (anchors[middle].timestamp! < span.begin) low = middle + 1;
      else high = middle;
    }
    const next = low === anchors.length ? -1 : low;
    if (next === 0)
      return positions.get(anchors[0].line)! - (span.begin! < anchors[0].timestamp! ? 1 : 0);
    if (next < 0) return own;
    const before = anchors[next - 1],
      after = anchors[next];
    const fraction = (span.begin - before.timestamp!) / (after.timestamp! - before.timestamp!);
    return (
      positions.get(before.line)! +
      (positions.get(after.line)! - positions.get(before.line)!) * fraction
    );
  };
  const endPosition = (span: LogExecution) =>
    span.end ? positions.get(span.end.line)! : undefined;
  for (const span of spans)
    span.orderedWithoutTime = span.begin === undefined || (!!span.end && span.finish === undefined);
  const allSpans = spans.slice();
  spans.sort(
    (a, b) => beginPosition(a) - beginPosition(b) || rank(a.event) - rank(b.event) || a.id - b.id,
  );
  spans.splice(Math.max(1, Math.min(100, limit)));
  for (const span of spans) {
    if (span.begin !== undefined && span.finish !== undefined && span.finish < span.begin) {
      span.ambiguous = true;
      span.finish = undefined;
    }
    const record = span.start ?? span.end!;
    const parentSpanId = text(record.fields.parentSpanId ?? span.end?.fields.parentSpanId);
    if (parentSpanId) {
      const traceId = text(record.fields.traceId);
      const candidates = spans.filter((parent) => {
        const parentRecord = parent.start ?? parent.end!;
        return (
          parent !== span &&
          text(parentRecord.fields.spanId) === parentSpanId &&
          text(parentRecord.fields.traceId) === traceId
        );
      });
      if (candidates.length === 1) span.parent = candidates[0].id;
      else if (candidates.length > 1) span.ambiguous = true;
      // Explicit correlation is authoritative, including when its parent is omitted.
      continue;
    }
    const parentId = text(record.fields.parentRequestId);
    if (parentId) {
      const candidates = spans.filter(
        (parent) =>
          parent !== span &&
          text((parent.start ?? parent.end!).fields.requestId) === parentId &&
          parent.begin !== undefined &&
          span.begin !== undefined &&
          parent.begin <= span.begin &&
          (parent.finish === undefined || parent.finish >= (span.finish ?? span.begin)),
      );
      if (candidates.length === 1) {
        span.parent = candidates[0].id;
        continue;
      }
    }
    const uri = text(record.fields.uri);
    if (!uri || span.begin === undefined || span.ambiguous) continue;
    const candidates = spans.filter(
      (parent) =>
        parent !== span &&
        !parent.ambiguous &&
        rank(parent.event) < rank(span.event) &&
        parent.begin !== undefined &&
        parent.finish !== undefined &&
        parent.begin <= span.begin! &&
        parent.finish >= (span.finish ?? span.begin!) &&
        text((parent.start ?? parent.end!).fields.uri) === uri,
    );
    const closest = Math.max(-1, ...candidates.map((parent) => rank(parent.event)));
    const parents = candidates.filter((parent) => rank(parent.event) === closest);
    if (parents.length === 1) {
      span.parent = parents[0].id;
      span.inferredParent = true;
    }
  }
  // Started/ended intervals can establish containment in Output logs too.
  // This remains an inference, not proof of causality between parallel tasks.
  for (const span of spans) {
    if (
      span.parent !== undefined ||
      span.ambiguous ||
      text((span.start ?? span.end!).fields.parentSpanId ?? span.end?.fields.parentSpanId)
    )
      continue;
    const record = span.start ?? span.end!;
    const uri = text(record.fields.uri);
    if (!uri) continue;
    const parents = spans.filter(
      (parent) =>
        parent !== span &&
        !parent.ambiguous &&
        parent.start &&
        parent.end &&
        rank(parent.event) < rank(span.event) &&
        text(parent.start.fields.uri) === uri &&
        parent.start.line < record.line &&
        parent.end.line >= (span.end ?? record).line,
    );
    const closest = Math.max(-1, ...parents.map((parent) => rank(parent.event)));
    const candidates = parents.filter((parent) => rank(parent.event) === closest);
    if (candidates.length === 1) {
      span.parent = candidates[0].id;
      span.inferredParent = true;
    }
  }
  // Guard malformed parent metadata against cycles without inventing an order.
  const byId = new Map(spans.map((span) => [span.id, span]));
  for (const span of spans) {
    const visited = new Set<number>([span.id]);
    let parent = span.parent;
    while (parent !== undefined) {
      if (visited.has(parent)) {
        span.parent = undefined;
        span.ambiguous = true;
        break;
      }
      visited.add(parent);
      parent = byId.get(parent)?.parent;
    }
  }
  const visible = spans;
  const times = [
    ...new Set(
      visible.flatMap((span) =>
        [beginPosition(span), endPosition(span)].filter(
          (value): value is number => value !== undefined,
        ),
      ),
    ),
  ].sort((a, b) => a - b);
  const slots = new Map(times.map((time, index) => [time, index * 2]));
  const ticks = entries
    .filter((entry) => entry.timestamp !== undefined && slots.has(positions.get(entry.line)!))
    .map((entry) => ({
      time: entry.timestamp!,
      top: 24 + slots.get(positions.get(entry.line)!)! * 104,
    }));
  const laneEnds: number[] = [];
  for (const span of visible) {
    const start = slots.get(beginPosition(span))!;
    const end =
      endPosition(span) === undefined
        ? start
        : Math.max(start, slots.get(endPosition(span)!)!) +
          (span.start || (span.estimated && !span.orderedWithoutTime) ? 1 : 0);
    let lane = laneEnds.findIndex((value) => value < start);
    if (lane < 0) lane = laneEnds.length;
    laneEnds[lane] = span.start && !span.end ? times.length * 2 : end;
    span.lane = lane;
    span.top = 24 + start * 104;
    span.bottom = 24 + Math.max(start, end) * 104;
  }
  return {
    spans: visible,
    omitted: Math.max(0, allSpans.length - visible.length),
    ticks,
    lanes: laneEnds.length,
    height: Math.max(180, ...visible.map((span) => span.bottom + 120)),
  };
}
