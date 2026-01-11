import React from "react";
import { useAuth } from "../contexts/AuthContext";
import { Plan } from "../api-types";
import { Link } from "react-router-dom";
import { User, Mail, CreditCard, Trash2, Shield, Camera } from "lucide-react";

const ProfilePage: React.FC = () => {
  const { user } = useAuth();
  const initial = user?.email?.[0]?.toUpperCase() || "U";

  return (
    <div className="max-w-4xl mx-auto space-y-8 animate-fade-in">
      {/* Page Header */}
      <div>
        <h1 className="text-3xl font-bold tracking-tight text-white mb-2">
          Account Settings
        </h1>
        <p className="text-neutral-400">
          Manage your personal information and security preferences.
        </p>
      </div>

      {/* General Info Card */}
      <div className="rounded-xl border border-neutral-800 bg-[#0a0a0a] overflow-hidden">
        <div className="p-6 border-b border-neutral-800">
          <h2 className="text-lg font-semibold text-white">
            General Information
          </h2>
          <p className="text-sm text-neutral-500 mt-1">
            Update your photo and personal details.
          </p>
        </div>

        <div className="p-6 space-y-8">
          {/* Avatar Section */}
          <div className="flex flex-col sm:flex-row items-center gap-6">
            <div className="relative group cursor-pointer">
              <div className="w-24 h-24 rounded-full bg-gradient-to-br from-neutral-700 to-neutral-600 flex items-center justify-center text-4xl font-bold text-white ring-4 ring-black shadow-xl">
                {initial}
              </div>
              <div className="absolute inset-0 rounded-full bg-black/50 flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity">
                <Camera className="w-6 h-6 text-white" />
              </div>
            </div>
            <div className="text-center sm:text-left">
              <button className="px-4 py-2 bg-white text-black text-sm font-medium rounded-md hover:bg-neutral-200 transition-colors shadow-[0_0_15px_-3px_rgba(255,255,255,0.3)]">
                Upload New Avatar
              </button>
              <p className="mt-2 text-xs text-neutral-500">
                JPG, GIF or PNG. Max size of 800K.
              </p>
            </div>
          </div>

          <div className="border-t border-neutral-800/50" />

          {/* Form Fields */}
          <div className="grid gap-6 max-w-xl">
            <div>
              <label className="block text-xs font-semibold text-neutral-400 uppercase tracking-wider mb-2">
                Email Address
              </label>
              <div className="flex">
                <div className="inline-flex items-center px-3 rounded-l-md border border-r-0 border-neutral-800 bg-neutral-900 text-neutral-500">
                  <Mail className="w-4 h-4" />
                </div>
                <input
                  type="email"
                  value={user?.email}
                  disabled
                  className="w-full bg-[#050505] border border-neutral-800 rounded-r-md px-3 py-2 text-sm text-neutral-400 focus:outline-none cursor-not-allowed"
                />
              </div>
              <p className="mt-2 text-xs text-neutral-500">
                Please contact support to change your email address.
              </p>
            </div>

            <div>
              <label className="block text-xs font-semibold text-neutral-400 uppercase tracking-wider mb-2">
                User ID
              </label>
              <div className="flex">
                <div className="inline-flex items-center px-3 rounded-l-md border border-r-0 border-neutral-800 bg-neutral-900 text-neutral-500">
                  <User className="w-4 h-4" />
                </div>
                <input
                  type="text"
                  // Mocking a UUID for visual completeness
                  value={`usr_${user?.email?.split("@")[0]}_8x9234`}
                  disabled
                  className="w-full bg-[#050505] border border-neutral-800 rounded-r-md px-3 py-2 text-sm text-neutral-400 font-mono focus:outline-none cursor-not-allowed"
                />
              </div>
            </div>
          </div>
        </div>
      </div>

      {/* Plan / Subscription Card */}
      <div className="rounded-xl border border-neutral-800 bg-[#0a0a0a] overflow-hidden">
        <div className="p-6 border-b border-neutral-800">
          <h2 className="text-lg font-semibold text-white">Plan & Usage</h2>
          <p className="text-sm text-neutral-500 mt-1">
            Manage your subscription tier.
          </p>
        </div>
        <div className="p-6">
          <div className="flex items-center justify-between p-4 rounded-lg bg-neutral-900/50 border border-neutral-800">
            <div className="flex items-center gap-4">
              <div
                className={`p-2 rounded-lg ${user?.plan === Plan.PAID ? "bg-purple-500/10 text-purple-400" : "bg-neutral-800 text-neutral-400"}`}
              >
                <CreditCard className="w-6 h-6" />
              </div>
              <div>
                <p className="text-white font-medium flex items-center gap-2">
                  {user?.plan === Plan.PAID ? "Vectron Pro" : "Hobby Plan"}
                  {user?.plan === Plan.PAID && (
                    <span className="px-2 py-0.5 rounded-full bg-purple-500/10 text-purple-400 text-[10px] border border-purple-500/20 font-bold uppercase">
                      Active
                    </span>
                  )}
                </p>
                <p className="text-sm text-neutral-500">
                  {user?.plan === Plan.PAID
                    ? "Your next billing date is February 1, 2026."
                    : "Upgrade to Pro for unlimited collections and higher throughput."}
                </p>
              </div>
            </div>
            <Link
              to="/dashboard/billing"
              className="text-sm font-medium text-white hover:text-neutral-300 border border-neutral-700 bg-neutral-800 px-3 py-1.5 rounded hover:bg-neutral-700 transition-colors"
            >
              Manage Subscription
            </Link>
          </div>
        </div>
      </div>

      {/* Danger Zone */}
      <div className="rounded-xl border border-red-900/20 bg-red-950/5 overflow-hidden">
        <div className="p-6 border-b border-red-900/10">
          <h2 className="text-lg font-semibold text-red-500 flex items-center gap-2">
            <Shield className="w-5 h-5" /> Danger Zone
          </h2>
        </div>
        <div className="p-6 flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4">
          <div>
            <p className="text-sm text-white font-medium">Delete Account</p>
            <p className="text-sm text-neutral-500 mt-1">
              Permanently delete your account and all of your content. This
              action is not reversible.
            </p>
          </div>
          <button className="px-4 py-2 bg-red-600 hover:bg-red-700 text-white text-sm font-medium rounded transition-colors shadow-lg shadow-red-900/20 flex items-center gap-2">
            <Trash2 className="w-4 h-4" />
            Delete Account
          </button>
        </div>
      </div>
    </div>
  );
};

export default ProfilePage;
