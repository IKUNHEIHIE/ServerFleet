import { useId, type ComponentProps, type ReactNode } from "react";
import { cn } from "@/lib/utils";

export const controlClassName =
  "mt-1 w-full rounded-lg border border-input bg-card px-3 py-2 text-sm text-foreground shadow-[0_1px_2px_rgba(28,32,48,0.03)] transition-all " +
  "placeholder:text-muted-foreground/75 hover:border-[#6658E8]/40 focus-visible:border-[#6658E8] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/25 " +
  "disabled:cursor-not-allowed disabled:bg-secondary/60 disabled:opacity-50";

export interface FieldProps {
  readonly label: string;
  readonly hint?: ReactNode;
  readonly error?: string | null;
  readonly children: (ids: {
    readonly inputId: string;
    readonly hintId: string;
    readonly errorId: string;
  }) => ReactNode;
}

export function Field({ label, hint, error, children }: FieldProps) {
  const base = useId();
  const inputId = `${base}-input`;
  const hintId = `${base}-hint`;
  const errorId = `${base}-error`;
  return (
    <div>
      <label htmlFor={inputId} className="block text-sm font-medium text-foreground">
        {label}
      </label>
      {children({ inputId, hintId, errorId })}
      {hint ? (
        <p id={hintId} className="mt-1 text-xs leading-relaxed text-muted-foreground">
          {hint}
        </p>
      ) : null}
      {error ? (
        <p id={errorId} role="alert" className="mt-1 text-xs font-medium text-destructive">
          {error}
        </p>
      ) : null}
    </div>
  );
}

export function TextInput({ className, ...props }: ComponentProps<"input">) {
  return <input {...props} className={cn(controlClassName, className)} />;
}

export function TextArea({ className, ...props }: ComponentProps<"textarea">) {
  return <textarea {...props} className={cn(controlClassName, "min-h-24", className)} />;
}

export function Select({ className, children, ...props }: ComponentProps<"select">) {
  return (
    <select {...props} className={cn(controlClassName, className)}>
      {children}
    </select>
  );
}
