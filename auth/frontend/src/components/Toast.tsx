import React, { useState, useEffect } from "react";
import { X, CheckCircle, AlertCircle, Info } from "lucide-react";

interface ToastProps {
  message: string | null;
  type: "success" | "danger" | "info";
  onClose: () => void;
  duration?: number;
}

export const Toast: React.FC<ToastProps> = ({
  message,
  type,
  onClose,
  duration = 4000,
}) => {
  const [isVisible, setIsVisible] = useState(false);
  const [shouldRender, setShouldRender] = useState(false);

  useEffect(() => {
    if (message) {
      setShouldRender(true);
      // Small timeout to allow the render to happen before fading in
      const showTimer = setTimeout(() => setIsVisible(true), 10);

      const hideTimer = setTimeout(() => {
        setIsVisible(false);
      }, duration);

      return () => {
        clearTimeout(showTimer);
        clearTimeout(hideTimer);
      };
    }
  }, [message, duration]);

  // Handle unmounting after animation completes
  useEffect(() => {
    if (!isVisible && shouldRender) {
      const timer = setTimeout(() => {
        setShouldRender(false);
        onClose();
      }, 300); // Match transition duration
      return () => clearTimeout(timer);
    }
  }, [isVisible, shouldRender, onClose]);

  if (!shouldRender || !message) return null;

  // Design Configuration
  const config = {
    success: {
      icon: <CheckCircle className="w-5 h-5 text-green-500" />,
      border: "border-green-500/20",
      bg: "bg-green-500/10", // Very subtle glow
    },
    danger: {
      icon: <AlertCircle className="w-5 h-5 text-red-500" />,
      border: "border-red-500/20",
      bg: "bg-red-500/10",
    },
    info: {
      icon: <Info className="w-5 h-5 text-white" />,
      border: "border-neutral-700",
      bg: "bg-white/5",
    },
  }[type];

  return (
    <div
      className={`
            fixed top-6 right-6 z-50
            transition-all duration-300 ease-out transform
            ${isVisible ? "translate-y-0 opacity-100" : "-translate-y-4 opacity-0"}
        `}
    >
      <div
        className={`
                flex items-start gap-4 p-4 rounded-xl
                bg-black/90 backdrop-blur-md
                border ${config.border}
                shadow-[0_8px_30px_rgb(0,0,0,0.5)]
                min-w-[320px] max-w-md
            `}
      >
        <div className="flex-shrink-0 mt-0.5">{config.icon}</div>

        <div className="flex-1">
          <p className="text-sm font-medium text-white leading-relaxed">
            {message}
          </p>
        </div>

        <button
          onClick={() => setIsVisible(false)}
          className="flex-shrink-0 text-neutral-500 hover:text-white transition-colors"
        >
          <X className="w-4 h-4" />
        </button>
      </div>
    </div>
  );
};
