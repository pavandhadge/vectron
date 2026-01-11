import React, { useState } from "react";
import {
  Terminal,
  Box,
  Cpu,
  Copy,
  Check,
  ArrowRight,
  BookOpen,
  Code,
  Coffee,
} from "lucide-react";

// --- Interfaces ---

interface SDK {
  id: string;
  language: string;
  description: string;
  icon: React.ReactNode;
  installCommand: string;
  docsLink: string;
  version: string;
}

// --- Data ---

const sdks: SDK[] = [
  {
    id: "js",
    language: "Node.js / TypeScript",
    description:
      "Type-safe client with automatic retry logic and connection pooling.",
    icon: <Box className="w-6 h-6 text-yellow-400" />,
    installCommand: "npm install @vectron/node",
    docsLink: "/docs/node",
    version: "v1.2.4",
  },
  {
    id: "py",
    language: "Python",
    description:
      "Pandas-compatible client optimized for data science workflows.",
    icon: <Terminal className="w-6 h-6 text-blue-400" />,
    installCommand: "pip install vectron-client",
    docsLink: "/docs/python",
    version: "v2.0.1",
  },
  {
    id: "go",
    language: "Go",
    description: "High-performance gRPC client for backend systems.",
    icon: <Cpu className="w-6 h-6 text-cyan-400" />,
    installCommand: "go get github.com/vectron/client-go",
    docsLink: "/docs/go",
    version: "v1.0.0",
  },
];

const upcoming = [
  { name: "Rust", icon: <Code className="w-5 h-5" /> },
  { name: "Java", icon: <Coffee className="w-5 h-5" /> },
];

// --- Components ---

const CommandBlock = ({ command }: { command: string }) => {
  const [copied, setCopied] = useState(false);

  const handleCopy = () => {
    navigator.clipboard.writeText(command);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div className="flex items-center justify-between bg-black border border-neutral-800 rounded-md p-3 group-hover:border-neutral-700 transition-colors">
      <code className="text-sm font-mono text-neutral-300">
        <span className="text-neutral-500 select-none">$ </span>
        {command}
      </code>
      <button
        onClick={handleCopy}
        className="p-1.5 text-neutral-500 hover:text-white hover:bg-neutral-800 rounded transition-colors"
        title="Copy command"
      >
        {copied ? (
          <Check size={14} className="text-green-500" />
        ) : (
          <Copy size={14} />
        )}
      </button>
    </div>
  );
};

const SDKCard = ({ sdk }: { sdk: SDK }) => (
  <div className="group flex flex-col justify-between p-6 rounded-xl border border-neutral-800 bg-[#0a0a0a] hover:bg-[#0a0a0a]/80 transition-all hover:shadow-lg hover:shadow-purple-900/5">
    <div>
      <div className="flex items-start justify-between mb-4">
        <div className="p-2.5 rounded-lg bg-neutral-900 border border-neutral-800 group-hover:border-neutral-700 transition-colors">
          {sdk.icon}
        </div>
        <span className="px-2 py-1 rounded text-[10px] font-mono font-medium bg-neutral-900 text-neutral-500 border border-neutral-800">
          {sdk.version}
        </span>
      </div>

      <h3 className="text-lg font-bold text-white mb-2">{sdk.language}</h3>
      <p className="text-sm text-neutral-400 mb-6 leading-relaxed">
        {sdk.description}
      </p>
    </div>

    <div className="space-y-4">
      <CommandBlock command={sdk.installCommand} />

      <a
        href={sdk.docsLink}
        className="flex items-center justify-center gap-2 w-full py-2 text-sm font-medium text-neutral-300 hover:text-white hover:bg-neutral-800 rounded-md transition-colors"
      >
        <BookOpen size={16} />
        View Documentation
      </a>
    </div>
  </div>
);

const SDKDownloadPage: React.FC = () => {
  return (
    <div className="max-w-6xl mx-auto space-y-12 animate-fade-in">
      {/* Header */}
      <div>
        <h1 className="text-3xl font-bold tracking-tight text-white mb-3">
          Developer SDKs
        </h1>
        <p className="text-neutral-400 max-w-2xl">
          Integrate Vectron into your application with our official libraries.
          All SDKs are type-safe and support connection pooling out of the box.
        </p>
      </div>

      {/* Main Grid */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
        {sdks.map((sdk) => (
          <SDKCard key={sdk.id} sdk={sdk} />
        ))}
      </div>

      {/* Upcoming / Community */}
      <div className="border-t border-neutral-800 pt-10">
        <h2 className="text-lg font-semibold text-white mb-6">Coming Soon</h2>
        <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-4 gap-4">
          {upcoming.map((item) => (
            <div
              key={item.name}
              className="flex items-center gap-3 p-4 rounded-lg border border-neutral-800 bg-neutral-900/30 text-neutral-500 cursor-not-allowed border-dashed"
            >
              {item.icon}
              <span className="text-sm font-medium">{item.name}</span>
              <span className="ml-auto text-[10px] uppercase tracking-wider bg-neutral-800 px-1.5 py-0.5 rounded text-neutral-600">
                Q1 2026
              </span>
            </div>
          ))}
          <a
            href="#"
            className="flex items-center gap-3 p-4 rounded-lg border border-neutral-800 bg-neutral-900/30 text-neutral-400 hover:text-white hover:border-neutral-700 transition-colors group"
          >
            <ArrowRight className="w-5 h-5 group-hover:translate-x-1 transition-transform" />
            <span className="text-sm font-medium">Request a Language</span>
          </a>
        </div>
      </div>
    </div>
  );
};

export default SDKDownloadPage;
