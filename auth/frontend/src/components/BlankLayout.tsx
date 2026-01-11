import React from "react";

interface BlankLayoutProps {
  children: React.ReactNode;
}

export const BlankLayout: React.FC<BlankLayoutProps> = ({ children }) => {
  return <>{children}</>;
};
