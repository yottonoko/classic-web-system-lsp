import { type JSX } from "@solidjs/web";

export function SectionHeading(props: { children: JSX.Element }): JSX.Element {
  return (
    <h2 class="mb-2 mt-3 text-xs font-semibold uppercase tracking-wide text-[#91a4bb]">
      {props.children}
    </h2>
  );
}
export function EmptyText(props: { children: JSX.Element }): JSX.Element {
  return (
    <div class="mb-3 rounded border border-dashed border-[#2f3d50] p-2 text-xs text-[#8190a4]">
      {props.children}
    </div>
  );
}
