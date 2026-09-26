import { onCleanup, createUniqueId } from "solid-js";
import type { JSX } from "@solidjs/web";

/** Accessible help with a hover/focus preview and a persistent modal on click. */
export function LogHint(props: { label: string; children: JSX.Element }): JSX.Element {
  let dialog!: HTMLDialogElement;
  let preview!: HTMLDivElement;
  let button!: HTMLButtonElement;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const id = createUniqueId();
  let suppressFocusPreview = false;
  const hide = () => {
    clearTimeout(timer);
    preview?.hidePopover();
  };
  const delayHide = () => {
    timer = setTimeout(hide, 160);
  };
  const show = () => {
    clearTimeout(timer);
    if (dialog.open) return;
    preview.showPopover();
    const rect = button.getBoundingClientRect();
    preview.style.left = `${Math.max(8, Math.min(rect.left, innerWidth - preview.offsetWidth - 8))}px`;
    preview.style.top = `${rect.bottom + 8 + preview.offsetHeight > innerHeight ? Math.max(8, rect.top - preview.offsetHeight - 8) : rect.bottom + 8}px`;
  };
  onCleanup(() => clearTimeout(timer));
  return (
    <span class="hint">
      <button
        ref={(element) => {
          button = element;
        }}
        type="button"
        class="hint-button"
        aria-label={props.label}
        aria-describedby={id}
        onMouseEnter={show}
        onMouseLeave={delayHide}
        onFocus={() => {
          if (!suppressFocusPreview) show();
        }}
        onBlur={() => {
          hide();
          suppressFocusPreview = false;
        }}
        onKeyDown={(event) => {
          if (event.key === "Escape") hide();
        }}
        onClick={() => {
          hide();
          dialog.showModal();
          suppressFocusPreview = true;
        }}
      >
        ?
      </button>
      <div
        ref={(element) => {
          preview = element;
        }}
        id={id}
        role="tooltip"
        popover="manual"
        class="hint-preview"
        onMouseEnter={() => clearTimeout(timer)}
        onMouseLeave={delayHide}
      >
        {props.children}
      </div>
      <dialog
        ref={(element) => {
          dialog = element;
        }}
        aria-label={props.label}
      >
        <form method="dialog">
          <strong>{props.label}</strong>
          <button>{props.label === "Hint" ? "Close" : "閉じる"}</button>
        </form>
        <div class="hint-body">{props.children}</div>
      </dialog>
    </span>
  );
}
