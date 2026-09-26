import { createEffect, omit, type Accessor } from "solid-js";
import { Dynamic, type JSX } from "@solidjs/web";
import type { WebviewEvent, WebviewRef } from "./webview-dom-types";

type TextControlElement = HTMLInputElement | HTMLTextAreaElement;

export interface ImeCompositionSnapshot {
  selectionEnd: number;
  selectionStart: number;
  value: string;
}

function nativeEventIsComposing(event: WebviewEvent<Event, TextControlElement>): boolean {
  return (event as Event & { isComposing?: boolean }).isComposing === true;
}

export function imeSafeKeyboardEventIsComposing(
  event: KeyboardEvent | WebviewEvent<KeyboardEvent, Element>,
): boolean {
  const nativeEvent = event;
  const keyboardEvent = nativeEvent as KeyboardEvent & { isComposing?: boolean };
  return keyboardEvent.isComposing === true || keyboardEvent.keyCode === 229;
}

function inputEventCompositionText(
  event: WebviewEvent<Event, TextControlElement>,
): string | undefined {
  const nativeEvent = event as Event & {
    data?: string | null;
    inputType?: string;
    isComposing?: boolean;
  };
  return nativeEvent.isComposing === true &&
    nativeEvent.inputType === "insertCompositionText" &&
    typeof nativeEvent.data === "string" &&
    nativeEvent.data.length > 0
    ? nativeEvent.data
    : undefined;
}

function compositionSnapshotFor(element: TextControlElement): ImeCompositionSnapshot | undefined {
  const selectionStart = element.selectionStart;
  const selectionEnd = element.selectionEnd;
  if (selectionStart === null || selectionEnd === null) {
    return undefined;
  }
  return {
    selectionEnd,
    selectionStart,
    value: element.value,
  };
}

function snapshotHasSelection(
  snapshot: ImeCompositionSnapshot | undefined,
): snapshot is ImeCompositionSnapshot {
  return snapshot !== undefined && snapshot.selectionStart !== snapshot.selectionEnd;
}

function snapshotSelectionLength(snapshot: ImeCompositionSnapshot): number {
  return snapshot.selectionEnd - snapshot.selectionStart;
}

function snapshotValueWithSelectionReplacement(
  snapshot: ImeCompositionSnapshot,
  replacementText: string,
): string | undefined {
  if (replacementText.length === 0) {
    return undefined;
  }
  return `${snapshot.value.slice(0, snapshot.selectionStart)}${replacementText}${snapshot.value.slice(
    snapshot.selectionEnd,
  )}`;
}

function snapshotSelectionContainsRange(
  snapshot: ImeCompositionSnapshot,
  selectionStart: number,
  selectionEnd: number,
): boolean {
  return snapshot.selectionStart <= selectionStart && snapshot.selectionEnd >= selectionEnd;
}

function snapshotRangeIsInsideReplacement(
  currentSnapshot: ImeCompositionSnapshot | undefined,
  previousSelectionSnapshot: ImeCompositionSnapshot,
  replacementText: string,
  currentValue: string,
): boolean {
  if (!currentSnapshot) {
    return true;
  }
  if (currentSnapshot.value !== currentValue) {
    return false;
  }
  const replacementStart = previousSelectionSnapshot.selectionStart;
  const replacementEnd = replacementStart + replacementText.length;
  return (
    currentSnapshot.selectionStart >= replacementStart &&
    currentSnapshot.selectionEnd <= replacementEnd
  );
}

function shouldUsePreviousSelectionSnapshot(
  currentSnapshot: ImeCompositionSnapshot | undefined,
  previousSelectionSnapshot: ImeCompositionSnapshot | undefined,
  currentValue: string,
  compositionStartText: string,
): previousSelectionSnapshot is ImeCompositionSnapshot {
  if (!snapshotHasSelection(previousSelectionSnapshot)) {
    return false;
  }
  if (previousSelectionSnapshot.value !== currentValue) {
    return (
      snapshotValueWithSelectionReplacement(previousSelectionSnapshot, compositionStartText) ===
        currentValue &&
      snapshotRangeIsInsideReplacement(
        currentSnapshot,
        previousSelectionSnapshot,
        compositionStartText,
        currentValue,
      )
    );
  }
  if (!snapshotHasSelection(currentSnapshot)) {
    return true;
  }
  if (currentSnapshot.value !== currentValue) {
    return false;
  }
  return (
    snapshotSelectionLength(previousSelectionSnapshot) > snapshotSelectionLength(currentSnapshot) &&
    snapshotSelectionContainsRange(
      previousSelectionSnapshot,
      currentSnapshot.selectionStart,
      currentSnapshot.selectionEnd,
    )
  );
}

export function imeSafeCompositionStartSnapshot(
  currentSnapshot: ImeCompositionSnapshot | undefined,
  previousSelectionSnapshot: ImeCompositionSnapshot | undefined,
  currentValue: string,
  selectedText: string,
): ImeCompositionSnapshot | undefined {
  if (
    shouldUsePreviousSelectionSnapshot(
      currentSnapshot,
      previousSelectionSnapshot,
      currentValue,
      selectedText,
    )
  ) {
    return previousSelectionSnapshot;
  }
  if (snapshotHasSelection(currentSnapshot)) {
    return currentSnapshot;
  }
  if (selectedText.length > 0) {
    const selectionStart = currentValue.indexOf(selectedText);
    if (selectionStart >= 0) {
      return {
        selectionEnd: selectionStart + selectedText.length,
        selectionStart,
        value: currentValue,
      };
    }
  }
  return currentSnapshot;
}

export function imeSafeCommittedText(
  compositionEndText: string,
  latestCompositionText: string | undefined,
): string {
  if (compositionEndText.length === 0) {
    return latestCompositionText ?? "";
  }
  if (
    latestCompositionText &&
    latestCompositionText.length > compositionEndText.length &&
    /^[\x20-\x7e]+$/.test(latestCompositionText) &&
    /^[\x20-\x7e]+$/.test(compositionEndText) &&
    (latestCompositionText.startsWith(compositionEndText) ||
      latestCompositionText.endsWith(compositionEndText))
  ) {
    return latestCompositionText;
  }
  return compositionEndText;
}

function fallbackPreservesCompositionRemainder(
  snapshot: ImeCompositionSnapshot,
  committedText: string,
  fallbackValue: string,
): boolean {
  const prefix = snapshot.value.slice(0, snapshot.selectionStart);
  const suffix = snapshot.value.slice(snapshot.selectionEnd);
  if (!fallbackValue.startsWith(prefix) || !fallbackValue.endsWith(suffix)) {
    return false;
  }
  const middleEnd = fallbackValue.length - suffix.length;
  const middle = fallbackValue.slice(prefix.length, middleEnd);
  const selectedText = snapshotHasSelection(snapshot)
    ? snapshot.value.slice(snapshot.selectionStart, snapshot.selectionEnd)
    : committedText;
  const afterCommit = middle.startsWith(committedText)
    ? middle.slice(committedText.length)
    : undefined;
  const beforeCommit = middle.endsWith(committedText)
    ? middle.slice(0, middle.length - committedText.length)
    : undefined;
  return [afterCommit, beforeCommit].some(
    (remainder) =>
      remainder !== undefined && remainder.length > 0 && selectedText.includes(remainder),
  );
}

export function imeSafeCompositionEndValue(
  snapshot: ImeCompositionSnapshot | undefined,
  committedText: string,
  fallbackValue: string,
): string {
  if (!snapshot || committedText.length === 0) {
    return fallbackValue;
  }
  const nextValue = `${snapshot.value.slice(0, snapshot.selectionStart)}${committedText}${snapshot.value.slice(
    snapshot.selectionEnd,
  )}`;
  return fallbackValue === nextValue ||
    fallbackPreservesCompositionRemainder(snapshot, committedText, fallbackValue)
    ? nextValue
    : fallbackValue;
}

export function imeSafeShouldWriteExternalValue(
  currentValue: string,
  nextValue: string,
  lastEmittedValue: string | undefined,
  isComposing: boolean,
): boolean {
  if (isComposing || currentValue === nextValue) {
    return false;
  }
  return !(lastEmittedValue !== undefined && currentValue === lastEmittedValue);
}

function createImeSafeTextControl<T extends TextControlElement>(
  value: Accessor<string>,
  onValueChange: (value: string) => void,
): {
  elementRef: WebviewRef<T | null>;
  onBeforeInput(event: WebviewEvent<Event, T>): void;
  onChange(event: WebviewEvent<Event, T>): void;
  onCompositionEnd(event: WebviewEvent<CompositionEvent, T>): void;
  onCompositionStart(event: WebviewEvent<CompositionEvent, T>): void;
  onCompositionUpdate(event: WebviewEvent<CompositionEvent, T>): void;
  onSelect(event: WebviewEvent<Event, T>): void;
} {
  const elementRef: { current: T | null } = { current: null };
  const isComposingRef = { current: false };
  const compositionSnapshotRef: { current: ImeCompositionSnapshot | undefined } = {
    current: undefined,
  };
  const latestCompositionTextRef: { current: string | undefined } = { current: undefined };
  const lastEmittedValueRef: { current: string | undefined } = { current: undefined };
  const previousSelectionSnapshotRef: { current: ImeCompositionSnapshot | undefined } = {
    current: undefined,
  };
  const emitValueChange = (nextValue: string): void => {
    lastEmittedValueRef.current = nextValue;
    onValueChange(nextValue);
  };

  createEffect(value, (currentValue) => {
    const element = elementRef.current;
    if (!element) {
      return;
    }
    if (lastEmittedValueRef.current === currentValue) {
      lastEmittedValueRef.current = undefined;
    }
    if (
      imeSafeShouldWriteExternalValue(
        element.value,
        currentValue,
        lastEmittedValueRef.current,
        isComposingRef.current,
      )
    ) {
      element.value = currentValue;
    }
  });

  return {
    elementRef,
    onBeforeInput(event) {
      const nextText = inputEventCompositionText(event);
      if (nextText !== undefined) {
        latestCompositionTextRef.current = nextText;
      }
    },
    onChange(event) {
      if (!isComposingRef.current && !nativeEventIsComposing(event)) {
        previousSelectionSnapshotRef.current = undefined;
        emitValueChange(event.currentTarget.value);
      }
    },
    onCompositionEnd(event) {
      isComposingRef.current = false;
      const nextValue = imeSafeCompositionEndValue(
        compositionSnapshotRef.current,
        imeSafeCommittedText(event.data, latestCompositionTextRef.current),
        event.currentTarget.value,
      );
      compositionSnapshotRef.current = undefined;
      latestCompositionTextRef.current = undefined;
      previousSelectionSnapshotRef.current = undefined;
      if (event.currentTarget.value !== nextValue) {
        event.currentTarget.value = nextValue;
      }
      emitValueChange(nextValue);
    },
    onCompositionStart(event) {
      isComposingRef.current = true;
      latestCompositionTextRef.current = undefined;
      compositionSnapshotRef.current = imeSafeCompositionStartSnapshot(
        compositionSnapshotFor(event.currentTarget),
        previousSelectionSnapshotRef.current,
        event.currentTarget.value,
        event.data,
      );
    },
    onCompositionUpdate(event) {
      if (event.data.length > 0) {
        latestCompositionTextRef.current = event.data;
      }
    },
    onSelect(event) {
      const snapshot = compositionSnapshotFor(event.currentTarget);
      previousSelectionSnapshotRef.current = snapshotHasSelection(snapshot) ? snapshot : undefined;
    },
  };
}

type TextControlProps = {
  value: string;
  onValueChange(value: string): void;
};
type ImeSafeInputProps = Omit<JSX.IntrinsicElements["input"], "value" | "onInput" | "onChange"> &
  TextControlProps;
type ImeSafeTextareaProps = Omit<
  JSX.IntrinsicElements["textarea"],
  "value" | "onInput" | "onChange"
> &
  TextControlProps;

/** Preserve native composition text while applying external value changes after composition. */
export function ImeSafeInput(props: ImeSafeInputProps): JSX.Element {
  return <ImeTextControl kind="input" control={props} />;
}

/** Multiline input with the same composition and selection guarantees as ImeSafeInput. */
export function ImeSafeTextarea(props: ImeSafeTextareaProps): JSX.Element {
  return <ImeTextControl kind="textarea" control={props} />;
}

function ImeTextControl(props: {
  kind: "input" | "textarea";
  control: ImeSafeInputProps | ImeSafeTextareaProps;
}): JSX.Element {
  const control = createImeSafeTextControl<TextControlElement>(
    () => props.control.value,
    (value) => props.control.onValueChange(value),
  );
  const rest = omit(
    props.control,
    "value",
    "onValueChange",
    "ref",
    "onBeforeInput",
    "onCompositionStart",
    "onCompositionUpdate",
    "onCompositionEnd",
    "onSelect",
  );
  const forward = (
    name:
      | "onBeforeInput"
      | "onCompositionStart"
      | "onCompositionUpdate"
      | "onCompositionEnd"
      | "onSelect",
    event: Event,
  ) => {
    const handler = props.control[name];
    if (typeof handler === "function") (handler as (event: Event) => void)(event);
  };
  return (
    <Dynamic
      component={props.kind}
      {...rest}
      onInput={control.onChange}
      onBeforeInput={(event: WebviewEvent<Event, TextControlElement>) => {
        control.onBeforeInput(event);
        forward("onBeforeInput", event);
      }}
      onCompositionStart={(event: WebviewEvent<CompositionEvent, TextControlElement>) => {
        control.onCompositionStart(event);
        forward("onCompositionStart", event);
      }}
      onCompositionUpdate={(event: WebviewEvent<CompositionEvent, TextControlElement>) => {
        control.onCompositionUpdate(event);
        forward("onCompositionUpdate", event);
      }}
      onCompositionEnd={(event: WebviewEvent<CompositionEvent, TextControlElement>) => {
        control.onCompositionEnd(event);
        forward("onCompositionEnd", event);
      }}
      onSelect={(event: WebviewEvent<Event, TextControlElement>) => {
        control.onSelect(event);
        forward("onSelect", event);
      }}
      ref={(element: TextControlElement) => {
        control.elementRef.current = element;
        element.value = props.control.value;
        if (typeof props.control.ref === "function")
          (props.control.ref as (element: TextControlElement) => void)(element);
      }}
    />
  );
}
