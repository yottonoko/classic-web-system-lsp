import React, { useEffect, useRef } from "react";

type TextControlElement = HTMLInputElement | HTMLTextAreaElement;

function assignForwardedRef<T>(ref: React.ForwardedRef<T>, value: T | null): void {
  if (typeof ref === "function") {
    ref(value);
  } else if (ref) {
    ref.current = value;
  }
}

function useImeSafeTextControl<T extends TextControlElement>(
  value: string,
  onValueChange: (value: string) => void,
): {
  elementRef: React.RefObject<T | null>;
  onChange(event: React.ChangeEvent<T>): void;
  onCompositionEnd(event: React.CompositionEvent<T>): void;
  onCompositionStart(): void;
} {
  const elementRef = useRef<T>(null);
  const isComposingRef = useRef(false);

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
      if (!isComposingRef.current) {
        onValueChange(event.currentTarget.value);
      }
    },
    onCompositionEnd(event) {
      isComposingRef.current = false;
      onValueChange(event.currentTarget.value);
    },
    onCompositionStart() {
      isComposingRef.current = true;
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
          textControl.onCompositionStart();
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
          textControl.onCompositionStart();
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
