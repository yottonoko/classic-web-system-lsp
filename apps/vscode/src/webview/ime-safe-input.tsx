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

function shouldUsePreviousSelectionSnapshot(
  currentSnapshot: ImeCompositionSnapshot | undefined,
  previousSelectionSnapshot: ImeCompositionSnapshot | undefined,
  currentValue: string,
): previousSelectionSnapshot is ImeCompositionSnapshot {
  if (
    !snapshotHasSelection(previousSelectionSnapshot) ||
    previousSelectionSnapshot.value !== currentValue
  ) {
    return false;
  }
  if (!snapshotHasSelection(currentSnapshot)) {
    return true;
  }
  if (currentSnapshot.value !== currentValue) {
    return false;
  }
  return (
    snapshotSelectionLength(previousSelectionSnapshot) > snapshotSelectionLength(currentSnapshot) &&
    previousSelectionSnapshot.selectionStart <= currentSnapshot.selectionStart &&
    previousSelectionSnapshot.selectionEnd >= currentSnapshot.selectionEnd
  );
}

export function imeSafeCompositionStartSnapshot(
  currentSnapshot: ImeCompositionSnapshot | undefined,
  previousSelectionSnapshot: ImeCompositionSnapshot | undefined,
  currentValue: string,
  selectedText: string,
): ImeCompositionSnapshot | undefined {
  if (
    shouldUsePreviousSelectionSnapshot(currentSnapshot, previousSelectionSnapshot, currentValue)
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

export function imeSafeCompositionEndValue(
  snapshot: ImeCompositionSnapshot | undefined,
  committedText: string,
  fallbackValue: string,
): string {
  if (!snapshot || committedText.length === 0) {
    return fallbackValue;
  }
  return `${snapshot.value.slice(0, snapshot.selectionStart)}${committedText}${snapshot.value.slice(
    snapshot.selectionEnd,
  )}`;
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
  const previousSelectionSnapshotRef = useRef<ImeCompositionSnapshot | undefined>(undefined);

  useEffect(() => {
    const element = elementRef.current;
    if (!element || isComposingRef.current || element.value === value) {
      return;
    }
    element.value = value;
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
        onValueChange(event.currentTarget.value);
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
      onValueChange(nextValue);
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
