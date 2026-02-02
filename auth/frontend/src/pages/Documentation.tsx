import { useState } from "react";
import {
  Book,
  Code,
  Terminal,
  Settings,
  Search,
  ChevronRight,
  Menu,
  Copy,
  Check,
  HelpCircle,
  Shield,
} from "lucide-react";

// --- MOCK DATA ---
const DOC_SECTIONS = [
  {
    category: "Getting Started",
    items: [
      {
        id: "intro",
        title: "Introduction",
        icon: <Book className="w-4 h-4" />,
      },
      {
        id: "setup",
        title: "Quick Setup",
        icon: <Terminal className="w-4 h-4" />,
      },
      {
        id: "auth",
        title: "Authentication",
        icon: <Shield className="w-4 h-4" />,
      },
    ],
  },
  {
    category: "Core Concepts",
    items: [
      {
        id: "vectors",
        title: "Vector Embeddings",
        icon: <Code className="w-4 h-4" />,
      },
      {
        id: "collections",
        title: "Collections",
        icon: <Settings className="w-4 h-4" />,
      },
    ],
  },
  {
    category: "Resources",
    items: [
      {
        id: "faq",
        title: "FAQ & Troubleshooting",
        icon: <HelpCircle className="w-4 h-4" />,
      },
    ],
  },
];

// Content Generator
const getContent = (id: string) => {
  switch (id) {
    case "intro":
      return (
        <div className="space-y-6 animate-fade-in">
          <h1 className="text-4xl font-bold tracking-tight mb-4">
            Introduction to Vectron
          </h1>
          <p className="text-lg text-neutral-400 leading-relaxed">
            Vectron is a high-performance, distributed vector database designed
            to handle billions of vectors with millisecond latency. It is built
            for developers who need to integrate semantic search, recommendation
            engines, and LLM memory into their applications.
          </p>
          <div className="p-4 bg-purple-900/10 border border-purple-500/20 rounded-lg">
            <h4 className="font-semibold text-purple-400 mb-2">Why Vectron?</h4>
            <ul className="list-disc list-inside text-neutral-300 space-y-1">
              <li>
                <strong className="text-white">Linearly Scalable:</strong> Add
                nodes to increase throughput.
              </li>
              <li>
                <strong className="text-white">Strong Consistency:</strong>{" "}
                Raft-based consensus ensures data safety.
              </li>
              <li>
                <strong className="text-white">Developer First:</strong> Native
                clients for TS, Python, and Go.
              </li>
            </ul>
          </div>
        </div>
      );
    case "setup":
      return (
        <div className="space-y-8 animate-fade-in">
          <div>
            <h1 className="text-4xl font-bold tracking-tight mb-4">
              Quick Setup
            </h1>
            <p className="text-neutral-400 mb-6">
              Get your local instance running in less than 30 seconds using
              Docker.
            </p>
          </div>

          <div className="space-y-4">
            <h3 className="text-xl font-semibold text-white">
              1. Pull the Docker Image
            </h3>
            <CodeBlock code="docker pull vectron/core:latest" />
          </div>

          <div className="space-y-4">
            <h3 className="text-xl font-semibold text-white">
              2. Start the Instance
            </h3>
            <p className="text-neutral-400">
              Run the following command to start a single-node cluster:
            </p>
            <CodeBlock
              code={`docker run -d -p 8080:8080 \\
  --name vectron-local \\
  -v vectron_data:/var/lib/vectron \\
  vectron/core:latest`}
            />
          </div>

          <div className="space-y-4">
            <h3 className="text-xl font-semibold text-white">
              3. Verify Installation
            </h3>
            <p className="text-neutral-400">Check the health endpoint:</p>
            <CodeBlock code="curl http://localhost:8080/v1/health" />
          </div>
        </div>
      );
    case "auth":
      return (
        <div className="space-y-8 animate-fade-in">
          <div>
            <h1 className="text-4xl font-bold tracking-tight mb-4">
              Authentication
            </h1>
            <p className="text-neutral-400 mb-6">
              Secure your database clusters using API keys and Role-Based Access
              Control (RBAC).
            </p>
          </div>

          <div className="space-y-4">
            <h3 className="text-xl font-semibold text-white">Using API Keys</h3>
            <p className="text-neutral-400">
              Include your API key in the{" "}
              <code className="bg-neutral-800 px-1 py-0.5 rounded text-sm text-purple-300">
                Authorization
              </code>{" "}
              header of every request.
            </p>
            <CodeBlock
              code={`// Headers
{
  "Authorization": "Bearer vk_live_5x9..."
}`}
            />
          </div>

          <div className="p-4 bg-yellow-900/10 border border-yellow-500/20 rounded-lg">
            <p className="text-yellow-200 text-sm">
              <strong>Warning:</strong> Never expose your secret API keys in
              client-side code. Always route requests through your backend.
            </p>
          </div>
        </div>
      );
    case "faq":
      return (
        <div className="space-y-8 animate-fade-in">
          <h1 className="text-4xl font-bold tracking-tight mb-8">
            FAQ & Troubleshooting
          </h1>

          {[
            {
              q: "How does Vectron handle consistency?",
              a: "Vectron uses the Raft consensus algorithm. All writes are replicated to a quorum of nodes before being acknowledged.",
            },
            {
              q: "Is there a limit on vector dimensions?",
              a: "The default limit is 4096 dimensions. This can be configured in your `vectron.yaml` file.",
            },
            {
              q: "Can I run Vectron on Kubernetes?",
              a: "Yes. We provide an official Helm chart and Operator for managing clusters on K8s.",
            },
            {
              q: "What distance metrics are supported?",
              a: "We currently support Cosine Similarity, Euclidean Distance (L2), and Dot Product.",
            },
          ].map((item, i) => (
            <div
              key={i}
              className="pb-6 border-b border-neutral-800 last:border-0"
            >
              <h3 className="text-lg font-medium text-white mb-2">{item.q}</h3>
              <p className="text-neutral-400">{item.a}</p>
            </div>
          ))}
        </div>
      );
    default:
      return (
        <div className="text-neutral-500">Select a topic from the sidebar.</div>
      );
  }
};

// --- HELPER COMPONENTS ---

const CodeBlock = ({ code }: { code: string }) => {
  const [copied, setCopied] = useState(false);

  const handleCopy = () => {
    navigator.clipboard.writeText(code);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div className="relative group rounded-lg overflow-hidden border border-neutral-800 bg-[#0a0a0a]">
      <div className="absolute top-3 right-3 opacity-0 group-hover:opacity-100 transition-opacity">
        <button
          onClick={handleCopy}
          className="p-1.5 rounded-md bg-neutral-800 text-neutral-400 hover:text-white transition-colors"
        >
          {copied ? (
            <Check className="w-4 h-4 text-green-500" />
          ) : (
            <Copy className="w-4 h-4" />
          )}
        </button>
      </div>
      <pre className="p-4 text-sm font-mono overflow-x-auto text-neutral-300 selection:bg-purple-500/30">
        <code>{code}</code>
      </pre>
    </div>
  );
};

// --- MAIN COMPONENT ---

export const Documentation = () => {
  const [activeSection, setActiveSection] = useState("intro");
  const [sidebarOpen, setSidebarOpen] = useState(false);

  return (
    <div className="min-h-screen bg-black text-white font-sans selection:bg-purple-500 selection:text-white">
      {/* Header / Navbar */}
      <header className="fixed top-0 z-50 w-full border-b border-neutral-800 bg-black/80 backdrop-blur-md">
        <div className="flex h-16 items-center px-4 sm:px-8 justify-between">
          <div className="flex items-center gap-4">
            <button
              className="lg:hidden p-2 text-neutral-400 hover:text-white"
              onClick={() => setSidebarOpen(!sidebarOpen)}
            >
              <Menu className="w-5 h-5" />
            </button>
            <div className="font-bold text-xl tracking-tight flex items-center gap-2">
              <div className="w-4 h-4 rounded-full bg-gradient-to-r from-purple-500 to-pink-500" />
              Vectron{" "}
              <span className="text-neutral-500 font-normal text-sm">Docs</span>
            </div>
          </div>

          <div className="relative hidden sm:block w-64">
            <Search className="absolute left-3 top-2.5 h-4 w-4 text-neutral-500" />
            <input
              type="text"
              placeholder="Search documentation..."
              className="h-9 w-full rounded-md border border-neutral-800 bg-neutral-900 pl-9 pr-4 text-sm text-neutral-300 placeholder:text-neutral-500 focus:border-purple-500 focus:outline-none focus:ring-1 focus:ring-purple-500 transition-all"
            />
          </div>
        </div>
      </header>

      <div className="flex pt-16 h-[calc(100vh)] overflow-hidden">
        {/* Sidebar */}
        <aside
          className={`
                    fixed inset-y-0 left-0 z-40 w-64 transform border-r border-neutral-800 bg-black transition-transform duration-300 lg:static lg:translate-x-0 pt-16 lg:pt-0
                    ${sidebarOpen ? "translate-x-0" : "-translate-x-full"}
                `}
        >
          <div className="h-full overflow-y-auto px-4 py-6 scrollbar-hide">
            {DOC_SECTIONS.map((section, idx) => (
              <div key={idx} className="mb-8">
                <h4 className="mb-3 px-2 text-xs font-semibold uppercase tracking-wider text-neutral-500">
                  {section.category}
                </h4>
                <div className="space-y-1">
                  {section.items.map((item) => (
                    <button
                      key={item.id}
                      onClick={() => {
                        setActiveSection(item.id);
                        setSidebarOpen(false);
                      }}
                      className={`
                                                group flex w-full items-center gap-3 rounded-md px-2 py-2 text-sm font-medium transition-colors
                                                ${
                                                  activeSection === item.id
                                                    ? "bg-purple-500/10 text-purple-400"
                                                    : "text-neutral-400 hover:bg-neutral-900 hover:text-white"
                                                }
                                            `}
                    >
                      {item.icon}
                      {item.title}
                      {activeSection === item.id && (
                        <ChevronRight className="ml-auto w-4 h-4 opacity-50" />
                      )}
                    </button>
                  ))}
                </div>
              </div>
            ))}
          </div>
        </aside>

        {/* Main Content */}
        <main className="flex-1 overflow-y-auto relative">
          {/* Background Gradient */}
          <div className="fixed top-0 right-0 w-[500px] h-[500px] bg-purple-900/10 blur-[100px] pointer-events-none" />

          <div className="mx-auto max-w-4xl px-6 py-12 lg:px-12 relative z-10">
            {/* Breadcrumb */}
            <div className="mb-6 flex items-center gap-2 text-sm text-neutral-500">
              <span>Docs</span>
              <ChevronRight className="w-3 h-3" />
              <span className="text-purple-400 capitalize">
                {
                  DOC_SECTIONS.find((c) =>
                    c.items.some((i) => i.id === activeSection),
                  )?.items.find((i) => i.id === activeSection)?.title
                }
              </span>
            </div>

            {getContent(activeSection)}

            {/* Page Feedback / Next Steps */}
            <div className="mt-20 border-t border-neutral-800 pt-10">
              <div className="flex justify-between items-center">
                <p className="text-sm text-neutral-500">
                  Was this page helpful?
                </p>
                <div className="flex gap-4">
                  <button className="text-xs border border-neutral-800 rounded-full px-4 py-2 hover:bg-neutral-900 transition">
                    Yes
                  </button>
                  <button className="text-xs border border-neutral-800 rounded-full px-4 py-2 hover:bg-neutral-900 transition">
                    No
                  </button>
                </div>
              </div>
            </div>
          </div>
        </main>
      </div>
    </div>
  );
};
