import { useEffect, useId, useRef, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { cn } from "@/lib/utils";
import { Button } from "./button";

export interface ModalProps {
  readonly open: boolean;
  readonly title: string;
  readonly description?: ReactNode;
  readonly onClose: () => void;
  readonly children: ReactNode;
  readonly footer?: ReactNode;
  readonly side?: boolean;
  readonly wide?: boolean;
}

/**
 * 模态对话框：焦点移入、Tab 循环、Esc 关闭、关闭后焦点回到触发元素。
 * 不引入弹窗库，避免新增共享依赖。
 */
export function Modal({ open, title, description, onClose, children, footer, side = false, wide = false }: ModalProps) {
  const titleId = useId();
  const descriptionId = useId();
  const panelRef = useRef<HTMLDivElement | null>(null);
  const restoreRef = useRef<HTMLElement | null>(null);
  const closeRef = useRef(onClose);
  closeRef.current = onClose;

  useEffect(() => {
    if (!open) return;
    restoreRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const panel = panelRef.current;
    const focusables = () =>
      panel
        ? Array.from(
            panel.querySelectorAll<HTMLElement>(
              'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
            ),
          ).filter(element => element.isConnected)
        : [];
    const first = focusables()[0] ?? panel;
    first?.focus();
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        closeRef.current();
        return;
      }
      if (event.key !== "Tab") return;
      const items = focusables();
      if (items.length === 0) return;
      const firstItem = items[0];
      const lastItem = items[items.length - 1];
      if (event.shiftKey && document.activeElement === firstItem) {
        event.preventDefault();
        lastItem.focus();
      } else if (!event.shiftKey && document.activeElement === lastItem) {
        event.preventDefault();
        firstItem.focus();
      }
    };
    document.addEventListener("keydown", onKeyDown, true);
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.removeEventListener("keydown", onKeyDown, true);
      document.body.style.overflow = previousOverflow;
      const restore = restoreRef.current;
      if (restore && restore.isConnected) restore.focus();
    };
  }, [open]);

  if (!open) return null;
  return createPortal(
    <div className="fixed inset-0 z-50 flex items-stretch justify-center bg-black/35 p-0 sm:items-center sm:p-6">
      <button type="button" aria-label="关闭对话框" tabIndex={-1} onClick={onClose} className="absolute inset-0 cursor-default" />
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={description ? descriptionId : undefined}
        tabIndex={-1}
        className={cn(
          "relative z-10 flex max-h-full w-full flex-col border bg-card shadow-xl focus:outline-none",
          side ? "ml-auto h-full max-w-xl sm:rounded-xl" : "sm:max-w-lg sm:rounded-xl",
          wide && "sm:max-w-4xl",
        )}
        style={side ? { marginRight: 0 } : undefined}
      >
        <header className="flex items-start justify-between gap-3 border-b px-5 py-4">
          <div className="min-w-0">
            <h2 id={titleId} className="font-semibold">
              {title}
            </h2>
            {description ? (
              <p id={descriptionId} className="mt-1 text-sm leading-6 text-muted-foreground">
                {description}
              </p>
            ) : null}
          </div>
          <Button type="button" size="sm" variant="ghost" onClick={onClose} aria-label="关闭">
            关闭
          </Button>
        </header>
        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">{children}</div>
        {footer ? <footer className="flex flex-wrap justify-end gap-2 border-t px-5 py-3">{footer}</footer> : null}
      </div>
    </div>,
    document.body,
  );
}
