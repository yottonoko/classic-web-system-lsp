import React, { useEffect, useRef } from "react";

type TextControlElement = HTMLInputElement | HTMLTextAreaElement;

export interface ImeCompositionSnapshot {
  selectionEnd: number;
  selectionStart: number;
  value: string;
}

function assignForwardedRef<T>(ref: React.ForwardedRef<T>, value: T | null): void {
  if (typeof ref === "function") {
    ref(value);
  } else if (ref) {
    ref.current = value;
  }
}

function nativeEventIsComposing(event: React.ChangeEvent<TextControlElement>): boolean {
  return (event.nativeEvent as Event & { isComposing?: boolean }).isComposing === true;
}

export function imeSafeKeyboardEventIsComposing(
  event: KeyboardEvent | React.KeyboardEvent<Element>,
): boolean {
  const nativeEvent = "nativeEvent" in event ? event.nativeEvent : event;
  const keyboardEvent = nativeEvent as KeyboardEvent & { isComposing?: boolean };
  return keyboardEvent.isComposing === true || keyboardEvent.keyCode === 229;
}

function inputEventCompositionText(event: React.FormEvent<TextControlElement>): string | undefined {
  const nativeEvent = event.nativeEvent as Event & {
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

function useImeSafeTextControl<T extends TextControlElement>(
  value: string,
  onValueChange: (value: string) => void,
): {
  elementRef: React.RefObject<T | null>;
  onBeforeInput(event: React.FormEvent<T>): void;
  onChange(event: React.ChangeEvent<T>): void;
  onCompositionEnd(event: React.CompositionEvent<T>): void;
  onCompositionStart(event: React.CompositionEvent<T>): void;
  onCompositionUpdate(event: React.CompositionEvent<T>): void;
  onSelect(event: React.SyntheticEvent<T>): void;
} {
  const elementRef = useRef<T>(null);
  const isComposingRef = useRef(false);
  const compositionSnapshotRef = useRef<ImeCompositionSnapshot | undefined>(undefined);
  const latestCompositionTextRef = useRef<string | undefined>(undefined);
  const lastEmittedValueRef = useRef<string | undefined>(undefined);
  const previousSelectionSnapshotRef = useRef<ImeCompositionSnapshot | undefined>(undefined);
  const emitValueChange = (nextValue: string): void => {
    lastEmittedValueRef.current = nextValue;
    onValueChange(nextValue);
  };

  useEffect(() => {
    const element = elementRef.current;
    if (!element) {
      return;
    }
    if (lastEmittedValueRef.current === value) {
      lastEmittedValueRef.current = undefined;
    }
    if (
      imeSafeShouldWriteExternalValue(
        element.value,
        value,
        lastEmittedValueRef.current,
        isComposingRef.current,
      )
    ) {
      element.value = value;
    }
  }, [value]);

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

type ImeSafeInputProps = Omit<
  React.InputHTMLAttributes<HTMLInputElement>,
  "defaultValue" | "onChange" | "value"
> & {
  onValueChange(value: string): void;
  value: string;
};

export const ImeSafeInput = React.forwardRef<HTMLInputElement, ImeSafeInputProps>(
  function ImeSafeInput(
    {
      onBeforeInput,
      onCompositionEnd,
      onCompositionStart,
      onCompositionUpdate,
      onSelect,
      onValueChange,
      value,
      ...props
    },
    forwardedRef,
  ): React.ReactElement {
    const textControl = useImeSafeTextControl<HTMLInputElement>(value, onValueChange);
    return (
      <input
        {...props}
        defaultValue={value}
        onBeforeInput={(event) => {
          textControl.onBeforeInput(event);
          onBeforeInput?.(event);
        }}
        onChange={textControl.onChange}
        onCompositionEnd={(event) => {
          textControl.onCompositionEnd(event);
          onCompositionEnd?.(event);
        }}
        onCompositionStart={(event) => {
          textControl.onCompositionStart(event);
          onCompositionStart?.(event);
        }}
        onCompositionUpdate={(event) => {
          textControl.onCompositionUpdate(event);
          onCompositionUpdate?.(event);
        }}
        onSelect={(event) => {
          textControl.onSelect(event);
          onSelect?.(event);
        }}
        ref={(element) => {
          textControl.elementRef.current = element;
          assignForwardedRef(forwardedRef, element);
        }}
      />
    );
  },
);

type ImeSafeTextareaProps = Omit<
  React.TextareaHTMLAttributes<HTMLTextAreaElement>,
  "defaultValue" | "onChange" | "value"
> & {
  onValueChange(value: string): void;
  value: string;
};

export const ImeSafeTextarea = React.forwardRef<HTMLTextAreaElement, ImeSafeTextareaProps>(
  function ImeSafeTextarea(
    {
      onBeforeInput,
      onCompositionEnd,
      onCompositionStart,
      onCompositionUpdate,
      onSelect,
      onValueChange,
      value,
      ...props
    },
    forwardedRef,
  ): React.ReactElement {
    const textControl = useImeSafeTextControl<HTMLTextAreaElement>(value, onValueChange);
    return (
      <textarea
        {...props}
        defaultValue={value}
        onBeforeInput={(event) => {
          textControl.onBeforeInput(event);
          onBeforeInput?.(event);
        }}
        onChange={textControl.onChange}
        onCompositionEnd={(event) => {
          textControl.onCompositionEnd(event);
          onCompositionEnd?.(event);
        }}
        onCompositionStart={(event) => {
          textControl.onCompositionStart(event);
          onCompositionStart?.(event);
        }}
        onCompositionUpdate={(event) => {
          textControl.onCompositionUpdate(event);
          onCompositionUpdate?.(event);
        }}
        onSelect={(event) => {
          textControl.onSelect(event);
          onSelect?.(event);
        }}
        ref={(element) => {
          textControl.elementRef.current = element;
          assignForwardedRef(forwardedRef, element);
        }}
      />
    );
  },
);
