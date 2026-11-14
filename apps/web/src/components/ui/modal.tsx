import { useEffect, useId, useRef, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { X } from "lucide-react";
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
 * 模态对话框 / 抽屉：焦点移入、Tab 循环、Esc 关闭、关闭后焦点回到触发元素。
 * 融合 NEUROX Light Theme 玻璃态遮罩与渐变顶部光带。
 */
export function Modal({
  open,
  title,
  description,
  onClose,
  children,
  footer,
  side = false,
  wide = false,
}: ModalProps) {
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
    <div className="fixed inset-0 z-50 flex items-stretch justify-center bg-[#1C2030]/40 p-0 backdrop-blur-xs sm:items-center sm:p-6">
      <button
        type="button"
        aria-label="关闭对话框"
        tabIndex={-1}
        onClick={onClose}
        className="absolute inset-0 cursor-default"
      />
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={description ? descriptionId : undefined}
        tabIndex={-1}
        className={cn(
          "relative z-10 flex max-h-full w-full flex-col overflow-hidden border border-border bg-card text-card-foreground shadow-[0_24px_64px_-12px_rgba(28,32,48,0.28)] focus:outline-none",
          side ? "ml-auto h-full max-w-xl sm:rounded-2xl" : "sm:max-w-lg sm:rounded-2xl",
          wide && "sm:max-w-4xl",
        )}
        style={side ? { marginRight: 0 } : undefined}
      >
        <div
          aria-hidden="true"
          className="pointer-events-none absolute inset-x-0 top-0 h-[3px] bg-gradient-to-r from-[#6658E8] via-[#3984E7] to-[#169B68]"
        />
        <header className="flex items-start justify-between gap-3 border-b border-border/80 bg-gradient-to-b from-[#FAFBFD] to-card px-5 py-4">
          <div className="min-w-0">
            <h2 id={titleId} className="font-bold tracking-tight text-foreground">
              {title}
            </h2>
            {description ? (
              <p id={descriptionId} className="mt-1 text-sm leading-6 text-muted-foreground">
                {description}
              </p>
            ) : null}
          </div>
          <Button type="button" size="sm" variant="ghost" onClick={onClose} aria-label="关闭">
            <X className="size-3.5" aria-hidden="true" />
            关闭
          </Button>
        </header>
        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">{children}</div>
        {footer ? (
          <footer className="flex flex-wrap justify-end gap-2 border-t border-border/80 bg-[#F6F7FB]/80 px-5 py-3">
            {footer}
          </footer>
        ) : null}
      </div>
    </div>,
    document.body,
  );
}
