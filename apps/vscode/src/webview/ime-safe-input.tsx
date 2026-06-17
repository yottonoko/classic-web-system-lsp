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
  onChange(event: React.ChangeEvent<T>): void;
  onCompositionEnd(event: React.CompositionEvent<T>): void;
  onCompositionStart(event: React.CompositionEvent<T>): void;
} {
  const elementRef = useRef<T>(null);
  const isComposingRef = useRef(false);
  const compositionSnapshotRef = useRef<ImeCompositionSnapshot | undefined>(undefined);

  useEffect(() => {
    const element = elementRef.current;
    if (!element || isComposingRef.current || element.value === value) {
      return;
    }
    element.value = value;
  }, [value]);

  return {
    elementRef,
    onChange(event) {
      if (!isComposingRef.current && !nativeEventIsComposing(event)) {
        onValueChange(event.currentTarget.value);
      }
    },
    onCompositionEnd(event) {
      isComposingRef.current = false;
      const nextValue = imeSafeCompositionEndValue(
        compositionSnapshotRef.current,
        event.data,
        event.currentTarget.value,
      );
      compositionSnapshotRef.current = undefined;
      if (event.currentTarget.value !== nextValue) {
        event.currentTarget.value = nextValue;
      }
      onValueChange(nextValue);
    },
    onCompositionStart(event) {
      isComposingRef.current = true;
      compositionSnapshotRef.current = compositionSnapshotFor(event.currentTarget);
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
    { onCompositionEnd, onCompositionStart, onValueChange, value, ...props },
    forwardedRef,
  ): React.ReactElement {
    const textControl = useImeSafeTextControl<HTMLInputElement>(value, onValueChange);
    return (
      <input
        {...props}
        defaultValue={value}
        onChange={textControl.onChange}
        onCompositionEnd={(event) => {
          textControl.onCompositionEnd(event);
          onCompositionEnd?.(event);
        }}
        onCompositionStart={(event) => {
          textControl.onCompositionStart(event);
          onCompositionStart?.(event);
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
    { onCompositionEnd, onCompositionStart, onValueChange, value, ...props },
    forwardedRef,
  ): React.ReactElement {
    const textControl = useImeSafeTextControl<HTMLTextAreaElement>(value, onValueChange);
    return (
      <textarea
        {...props}
        defaultValue={value}
        onChange={textControl.onChange}
        onCompositionEnd={(event) => {
          textControl.onCompositionEnd(event);
          onCompositionEnd?.(event);
        }}
        onCompositionStart={(event) => {
          textControl.onCompositionStart(event);
          onCompositionStart?.(event);
        }}
        ref={(element) => {
          textControl.elementRef.current = element;
          assignForwardedRef(forwardedRef, element);
        }}
      />
    );
  },
);
