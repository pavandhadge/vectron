import React from "react";
import { Outlet } from "react-router-dom";
import { NewNavbar } from "./NewNavbar";
import { NewSidebar } from "./NewSidebar";

export const NewLayout: React.FC = () => {
  const [mobileOpen, setMobileOpen] = React.useState(false);

  const handleDrawerToggle = () => {
    setMobileOpen(!mobileOpen);
  };

  return (
    <div className="flex min-h-screen bg-black text-white font-sans selection:bg-purple-500 selection:text-white">
      {/* 1. Sidebar */}
      {/* The Sidebar component handles its own 'fixed' positioning.
                We just need to ensure the mobile toggle state is passed down. */}
      <NewSidebar
        mobileOpen={mobileOpen}
        handleDrawerToggle={handleDrawerToggle}
      />

      {/* 2. Main Content Wrapper */}
      {/* 'sm:ml-64' creates the space for the fixed sidebar on desktop.
                'transition-all' ensures smooth resizing if you add a collapse feature later. */}
      <div className="flex-1 flex flex-col sm:ml-64 transition-all duration-300 ease-in-out min-h-screen relative">
        {/* 3. Navbar */}
        <NewNavbar handleDrawerToggle={handleDrawerToggle} />

        {/* 4. Page Content */}
        <main className="flex-1 relative z-0">
          {/* Aesthetic: Subtle Grid Background */}
          <div className="absolute inset-0 -z-10 h-full w-full bg-black bg-[linear-gradient(to_right,#80808012_1px,transparent_1px),linear-gradient(to_bottom,#80808012_1px,transparent_1px)] bg-[size:24px_24px]"></div>

          {/* Aesthetic: Top Spotlight Glow */}
          <div className="fixed top-0 left-1/2 -translate-x-1/2 w-[800px] h-[400px] bg-purple-900/10 blur-[100px] rounded-full pointer-events-none -z-10" />

          <div className="mx-auto max-w-7xl p-4 sm:p-6 lg:p-8">
            <Outlet />
          </div>
        </main>
      </div>
    </div>
  );
};
