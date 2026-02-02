import { useAuth } from "../contexts/AuthContext";
import { Menu, Search, Bell, HelpCircle, Command } from "lucide-react";

interface NewNavbarProps {
  handleDrawerToggle: () => void;
  // drawerWidth is handled via CSS classes in modern Tailwind layouts,
  // but kept here if you need dynamic inline styles.
  drawerWidth?: string;
}

export const NewNavbar: React.FC<NewNavbarProps> = ({ handleDrawerToggle }) => {
  // Note: We moved Logout to the Sidebar footer for a cleaner UI,
  // but the hook is here if you need to add a dropdown later.
  const { user } = useAuth();

  return (
    <nav
      className="
                sticky top-0 z-30
                flex h-16 w-full items-center gap-x-4
                border-b border-neutral-800
                bg-black/50 backdrop-blur-xl
                px-4 shadow-sm sm:gap-x-6 sm:px-6 lg:px-8
            "
    >
      {/* Mobile Menu Toggle */}
      <button
        type="button"
        className="-m-2.5 p-2.5 text-neutral-400 hover:text-white sm:hidden"
        onClick={handleDrawerToggle}
      >
        <span className="sr-only">Open sidebar</span>
        <Menu className="h-6 w-6" aria-hidden="true" />
      </button>

      {/* Separator for mobile */}
      <div className="h-6 w-px bg-neutral-800 sm:hidden" aria-hidden="true" />

      <div className="flex flex-1 gap-x-4 self-stretch lg:gap-x-6">
        {/* Search Bar (Visual Placeholder) */}
        <form className="relative flex flex-1" action="#" method="GET">
          <label htmlFor="search-field" className="sr-only">
            Search
          </label>
          <div className="relative w-full max-w-md text-neutral-500 focus-within:text-white">
            <div className="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-0">
              <Search className="h-4 w-4" aria-hidden="true" />
            </div>
            <input
              id="search-field"
              className="
                                block h-full w-full border-0 bg-transparent py-0 pl-8 pr-0
                                text-white placeholder:text-neutral-500
                                focus:ring-0 sm:text-sm
                            "
              placeholder="Search collections..."
              type="search"
              name="search"
            />
            <div className="hidden sm:flex absolute inset-y-0 right-0 items-center">
              <kbd className="inline-flex items-center gap-1 rounded border border-neutral-800 bg-neutral-900 px-2 py-0.5 text-xs font-light text-neutral-400 font-mono">
                <Command className="w-3 h-3" /> K
              </kbd>
            </div>
          </div>
        </form>

        {/* Right Side Actions */}
        <div className="flex items-center gap-x-4 lg:gap-x-6">
          {/* Documentation / Help */}
          <button
            type="button"
            className="-m-2.5 p-2.5 text-neutral-400 hover:text-white transition-colors"
          >
            <span className="sr-only">View documentation</span>
            <HelpCircle className="h-5 w-5" aria-hidden="true" />
          </button>

          {/* Notifications */}
          <button
            type="button"
            className="-m-2.5 p-2.5 text-neutral-400 hover:text-white transition-colors relative"
          >
            <span className="sr-only">View notifications</span>
            <Bell className="h-5 w-5" aria-hidden="true" />
            {/* Notification Dot */}
            <span className="absolute top-2 right-2.5 h-2 w-2 rounded-full bg-purple-500 ring-2 ring-black" />
          </button>

          {/* Separator */}
          <div
            className="hidden lg:block lg:h-6 lg:w-px lg:bg-neutral-800"
            aria-hidden="true"
          />

          {/* User Avatar (Simplified, as full profile is in Sidebar) */}
          <div className="flex items-center gap-x-4 lg:flex">
            <div className="flex items-center">
              <span className="hidden lg:flex lg:items-center">
                <span
                  className="text-sm font-semibold leading-6 text-white"
                  aria-hidden="true"
                >
                  {user?.email?.split("@")[0]}
                </span>
              </span>
            </div>
          </div>
        </div>
      </div>
    </nav>
  );
};
