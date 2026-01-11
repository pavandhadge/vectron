import React from "react";
import { NavLink, Link } from "react-router-dom";
import {
  LayoutDashboard,
  Key,
  FileText,
  Settings,
  Database,
  CreditCard,
  LogOut,
  ChevronRight,
  X,
} from "lucide-react";
import { useAuth } from "../contexts/AuthContext";

interface NewSidebarProps {
  drawerWidth?: string; // Made optional as we use Tailwind classes mostly
  mobileOpen: boolean;
  handleDrawerToggle: () => void;
}

export const NewSidebar: React.FC<NewSidebarProps> = ({
  mobileOpen,
  handleDrawerToggle,
}) => {
  const { user, logout } = useAuth();

  // Navigation Groups
  const mainNav = [
    { name: "Overview", to: "/dashboard", icon: LayoutDashboard, end: true },
    {
      name: "Collections",
      to: "/dashboard/collections",
      icon: Database,
      end: false,
    },
    { name: "API Keys", to: "/dashboard/keys", icon: Key, end: false },
    { name: "Profile", to: "/dashboard/profile", icon: Settings, end: false },
  ];

  const secondaryNav = [
    { name: "Billing", to: "/dashboard/billing", icon: CreditCard, end: false },
    { name: "Settings", to: "/dashboard/settings", icon: Settings, end: false },
    {
      name: "Documentation",
      to: "/dashboard/documentation",
      icon: FileText,
      end: false,
    },
  ];

  // Style Generators
  const getLinkClass = ({ isActive }: { isActive: boolean }) => `
        group flex items-center gap-3 px-3 py-2 rounded-md text-sm font-medium transition-all duration-200
        ${
          isActive
            ? "bg-[#1a1a1a] text-white shadow-[inset_0_1px_0_0_rgba(255,255,255,0.05)]"
            : "text-neutral-400 hover:text-white hover:bg-[#111]"
        }
    `;

  const SidebarContent = () => (
    <div className="flex flex-col h-full bg-black border-r border-neutral-800">
      {/* Header / Logo */}
      <div className="flex items-center h-16 px-6 border-b border-neutral-800/50">
        <Link to="/" className="flex items-center gap-2 group">
          <div className="w-5 h-5 rounded-full bg-gradient-to-tr from-purple-500 to-pink-500 group-hover:opacity-80 transition-opacity" />
          <span className="text-lg font-bold tracking-tight text-white">
            Vectron
          </span>
          <span className="px-1.5 py-0.5 text-[10px] font-medium bg-neutral-800 text-neutral-400 rounded border border-neutral-700 ml-2">
            BETA
          </span>
        </Link>
      </div>

      {/* Scrollable Nav Area */}
      <div className="flex-1 overflow-y-auto py-6 px-3 space-y-6">
        {/* Main Navigation */}
        <div>
          <h3 className="px-3 mb-2 text-xs font-semibold text-neutral-500 uppercase tracking-wider">
            Platform
          </h3>
          <nav className="space-y-1">
            {mainNav.map((item) => (
              <NavLink
                key={item.name}
                to={item.to}
                end={item.end}
                onClick={() => mobileOpen && handleDrawerToggle()} // Close on mobile click
                className={getLinkClass}
              >
                <item.icon className="w-4 h-4 opacity-70 group-hover:opacity-100 transition-opacity" />
                {item.name}
              </NavLink>
            ))}
          </nav>
        </div>

        {/* Secondary Navigation */}
        <div>
          <h3 className="px-3 mb-2 text-xs font-semibold text-neutral-500 uppercase tracking-wider">
            Support
          </h3>
          <nav className="space-y-1">
            {secondaryNav.map((item) => (
              <NavLink
                key={item.name}
                to={item.to}
                end={item.end}
                onClick={() => mobileOpen && handleDrawerToggle()}
                className={getLinkClass}
              >
                <item.icon className="w-4 h-4 opacity-70 group-hover:opacity-100 transition-opacity" />
                {item.name}
              </NavLink>
            ))}
          </nav>
        </div>
      </div>

      {/* User Profile / Footer */}
      <div className="p-4 border-t border-neutral-800 bg-neutral-900/10">
        <div className="flex items-center gap-3 mb-3">
          <div className="w-8 h-8 rounded-full bg-gradient-to-br from-neutral-700 to-neutral-600 flex items-center justify-center text-xs font-medium text-white ring-2 ring-black">
            {user?.email?.charAt(0).toUpperCase() || "U"}
          </div>
          <div className="flex-1 min-w-0">
            <p className="text-sm font-medium text-white truncate">
              {user?.email?.split("@")[0]}
            </p>
            <p className="text-xs text-neutral-500 truncate">Free Plan</p>
          </div>
        </div>
        <button
          onClick={logout}
          className="flex items-center justify-center gap-2 w-full px-3 py-1.5 text-xs font-medium text-neutral-400 hover:text-white border border-neutral-800 rounded hover:bg-neutral-800 transition-all"
        >
          <LogOut className="w-3 h-3" />
          Sign Out
        </button>
      </div>
    </div>
  );

  return (
    <>
      {/* Mobile Backdrop */}
      {mobileOpen && (
        <div
          className="fixed inset-0 z-40 bg-black/80 backdrop-blur-sm sm:hidden transition-opacity"
          onClick={handleDrawerToggle}
        />
      )}

      {/* Mobile Sidebar (Drawer) */}
      <div
        className={`
                    fixed inset-y-0 left-0 z-50 w-64 transform transition-transform duration-300 ease-in-out sm:hidden
                    ${mobileOpen ? "translate-x-0" : "-translate-x-full"}
                `}
      >
        <div className="absolute top-2 right-2 sm:hidden">
          <button
            onClick={handleDrawerToggle}
            className="p-2 text-neutral-400 hover:text-white"
          >
            <X className="w-5 h-5" />
          </button>
        </div>
        <SidebarContent />
      </div>

      {/* Desktop Sidebar */}
      <div className="hidden sm:block fixed inset-y-0 left-0 z-40 w-64">
        <SidebarContent />
      </div>
    </>
  );
};
