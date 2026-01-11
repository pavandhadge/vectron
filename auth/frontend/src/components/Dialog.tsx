import React, { useEffect, useRef } from "react";
import { X } from "lucide-react";

interface DialogProps {
  open: boolean;
  onClose: () => void;
  title: string;
  children: React.ReactNode;
  actions?: React.ReactNode;
}

export const Dialog: React.FC<DialogProps> = ({
  open,
  onClose,
  title,
  children,
  actions,
}) => {
  const dialogRef = useRef<HTMLDivElement>(null);

  // Handle Escape Key & Body Scroll Lock
  useEffect(() => {
    const handleEscape = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };

    if (open) {
      document.addEventListener("keydown", handleEscape);
      document.body.style.overflow = "hidden"; // Prevent background scrolling
    }

    return () => {
      document.removeEventListener("keydown", handleEscape);
      document.body.style.overflow = "unset";
    };
  }, [open, onClose]);

  // Handle Click Outside (Backdrop)
  const handleBackdropClick = (e: React.MouseEvent) => {
    if (dialogRef.current && !dialogRef.current.contains(e.target as Node)) {
      onClose();
    }
  };

  if (!open) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 sm:p-6">
      {/* Backdrop with Blur */}
      <div
        className="fixed inset-0 bg-black/60 backdrop-blur-sm transition-opacity"
        onClick={onClose}
        aria-hidden="true"
      />

      {/* Modal Content */}
      <div
        ref={dialogRef}
        className="
                    relative w-full max-w-lg transform overflow-hidden
                    rounded-xl border border-neutral-800 bg-[#0a0a0a]
                    p-6 text-left shadow-2xl transition-all
                    animate-in fade-in zoom-in-95 duration-200
                "
        role="dialog"
        aria-modal="true"
        aria-labelledby="modal-title"
      >
        {/* Header */}
        <div className="flex items-center justify-between mb-5">
          <h3
            id="modal-title"
            className="text-lg font-semibold leading-6 text-white tracking-tight"
          >
            {title}
          </h3>
          <button
            onClick={onClose}
            className="rounded-md p-1 text-neutral-500 hover:text-white hover:bg-neutral-800 transition-colors focus:outline-none focus:ring-2 focus:ring-purple-500"
          >
            <span className="sr-only">Close</span>
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Body */}
        <div className="mt-2 text-sm text-neutral-400 leading-relaxed">
          {children}
        </div>

        {/* Footer (Actions) */}
        {actions && (
          <div className="mt-8 flex flex-col-reverse sm:flex-row sm:justify-end sm:space-x-3 space-y-3 space-y-reverse sm:space-y-0">
            {actions}
          </div>
        )}
      </div>
    </div>
  );
};
