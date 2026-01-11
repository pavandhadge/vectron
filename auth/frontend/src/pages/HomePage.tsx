import React, { useState, useEffect, useRef } from "react";
import { Link, useNavigate } from "react-router-dom";
import {
  ChevronRight,
  Terminal,
  Zap,
  Layers,
  Shield,
  Globe,
  Cpu,
  Code,
  Check,
  Copy,
  ArrowRight,
  Server,
  Database,
  Box,
  Brain,
  Search,
  Sparkles,
  MessageSquare,
} from "lucide-react";
import { useAuth } from "../contexts/AuthContext";

// --- UTILS & MICRO-COMPONENTS ---

const SpotlightCard = ({
  children,
  className = "",
}: {
  children: React.ReactNode;
  className?: string;
}) => {
  const divRef = useRef<HTMLDivElement>(null);
  const [position, setPosition] = useState({ x: 0, y: 0 });
  const [opacity, setOpacity] = useState(0);

  const handleMouseMove = (e: React.MouseEvent<HTMLDivElement>) => {
    if (!divRef.current) return;
    const rect = divRef.current.getBoundingClientRect();
    setPosition({ x: e.clientX - rect.left, y: e.clientY - rect.top });
  };

  return (
    <div
      ref={divRef}
      onMouseMove={handleMouseMove}
      onMouseEnter={() => setOpacity(1)}
      onMouseLeave={() => setOpacity(0)}
      className={`relative rounded-xl border border-white/10 bg-neutral-900/40 overflow-hidden ${className}`}
    >
      <div
        className="pointer-events-none absolute -inset-px opacity-0 transition duration-300"
        style={{
          opacity,
          background: `radial-gradient(600px circle at ${position.x}px ${position.y}px, rgba(255,255,255,0.06), transparent 40%)`,
        }}
      />
      <div className="relative h-full">{children}</div>
    </div>
  );
};

const CopyButton = ({ text }: { text: string }) => {
  const [copied, setCopied] = useState(false);
  return (
    <button
      onClick={() => {
        navigator.clipboard.writeText(text);
        setCopied(true);
        setTimeout(() => setCopied(false), 2000);
      }}
      className="p-1.5 hover:bg-white/10 rounded transition-colors"
    >
      {copied ? (
        <Check size={14} className="text-green-400" />
      ) : (
        <Copy size={14} className="text-neutral-500" />
      )}
    </button>
  );
};

// --- SECTIONS ---

const Hero = () => (
  <section className="relative pt-32 pb-20 lg:pt-48 lg:pb-32 overflow-hidden">
    {/* Cinematic Background */}
    <div className="absolute inset-0 bg-[linear-gradient(to_right,#80808012_1px,transparent_1px),linear-gradient(to_bottom,#80808012_1px,transparent_1px)] bg-[size:24px_24px] [mask-image:radial-gradient(ellipse_60%_50%_at_50%_0%,#000_70%,transparent_100%)] pointer-events-none" />
    <div className="absolute top-0 left-1/2 -translate-x-1/2 w-[1000px] h-[400px] bg-purple-500/10 blur-[100px] rounded-full pointer-events-none" />

    <div className="container mx-auto px-6 relative z-10 text-center">
      <div className="inline-flex items-center gap-2 px-3 py-1 rounded-full border border-white/10 bg-white/5 backdrop-blur-md mb-8 animate-fade-in-up cursor-default hover:bg-white/10 transition-colors">
        <span className="flex h-2 w-2 rounded-full bg-green-500 shadow-[0_0_10px_rgba(34,197,94,0.5)]" />
        <span className="text-xs font-medium text-neutral-300 tracking-wide">
          Vectron Cloud Public Beta
        </span>
      </div>

      <h1 className="text-5xl sm:text-7xl lg:text-8xl font-bold tracking-tighter mb-8 text-white leading-[0.9]">
        Vector Search <br />
        <span className="text-transparent bg-clip-text bg-gradient-to-b from-white via-neutral-200 to-neutral-500">
          Reimagined.
        </span>
      </h1>

      <p className="text-lg text-neutral-400 max-w-2xl mx-auto mb-10 leading-relaxed">
        The distributed database built for the AI era.
        <strong className="text-white font-medium">
          {" "}
          Linearizable consistency
        </strong>{" "}
        meets
        <strong className="text-white font-medium">
          {" "}
          sub-millisecond latency
        </strong>
        .
      </p>

      <div className="flex flex-col sm:flex-row items-center justify-center gap-4">
        <Link
          to="/signup"
          className="h-12 px-8 rounded-full bg-white text-black font-semibold flex items-center gap-2 hover:bg-neutral-200 transition-all shadow-[0_0_30px_-5px_rgba(255,255,255,0.3)]"
        >
          Start Building <ArrowRight className="w-4 h-4" />
        </Link>
        <div className="h-12 px-6 rounded-full border border-white/10 bg-white/5 text-white font-mono text-sm flex items-center gap-3 backdrop-blur-md">
          <span>npm install @vectron/node</span>
          <CopyButton text="npm install @vectron/node" />
        </div>
      </div>
    </div>
  </section>
);

const TrustedBy = () => (
  <section className="border-y border-white/5 bg-black/50 py-10 overflow-hidden relative">
    <div className="absolute inset-y-0 left-0 w-32 bg-gradient-to-r from-black to-transparent z-10 pointer-events-none" />
    <div className="absolute inset-y-0 right-0 w-32 bg-gradient-to-l from-black to-transparent z-10 pointer-events-none" />

    <div className="container mx-auto px-6 text-center mb-6">
      <p className="text-xs font-medium text-neutral-500 uppercase tracking-[0.2em]">
        Trusted by engineering teams at
      </p>
    </div>

    {/* Simple Flex for Demo (In production, you'd use a CSS animation loop for infinite scroll) */}
    <div className="flex justify-center items-center gap-12 md:gap-24 opacity-40 grayscale hover:grayscale-0 transition-all duration-500">
      <div className="text-xl font-bold text-white flex items-center gap-2">
        <div className="w-5 h-5 bg-white rounded" /> ACME Corp
      </div>
      <div className="text-xl font-bold text-white font-serif italic">
        Stark
      </div>
      <div className="text-xl font-bold text-white font-mono">CYBERDYNE</div>
      <div className="text-xl font-bold text-white tracking-widest">
        WAYSTAR
      </div>
      <div className="text-xl font-bold text-white flex items-center gap-2">
        <Globe className="w-5 h-5" /> UMBRELLA
      </div>
    </div>
  </section>
);

const CodeExample = () => {
  const [activeTab, setActiveTab] = useState<"go" | "ts" | "py">("go");

  const codes = {
    go: `<span class="text-purple-400">package</span> main

<span class="text-purple-400">import</span> (
    <span class="text-green-400">"fmt"</span>
    <span class="text-green-400">"github.com/vectron/client-go"</span>
)

<span class="text-purple-400">func</span> <span class="text-blue-400">main</span>() {
    <span class="text-neutral-500">// 1. Initialize with type-safe config</span>
    client, _ := vectron.<span class="text-blue-400">NewClient</span>(
        <span class="text-green-400">"grpc.vectron.cloud:443"</span>,
        <span class="text-green-400">"vk_live_8x92..."</span>,
    )

    <span class="text-neutral-500">// 2. Create Collection (4 dims, Euclidean)</span>
    client.<span class="text-blue-400">CreateCollection</span>(<span class="text-green-400">"prod_vectors"</span>, <span class="text-yellow-400">4</span>, <span class="text-green-400">"euclidean"</span>)

    <span class="text-neutral-500">// 3. Upsert Points (Auto-batched)</span>
    points := []*vectron.Point{
        {ID: <span class="text-green-400">"vec_1"</span>, Vector: []<span class="text-yellow-400">float32</span>{<span class="text-yellow-400">0.1</span>, <span class="text-yellow-400">0.2</span>, <span class="text-yellow-400">0.3</span>, <span class="text-yellow-400">0.4</span>}},
    }
    client.<span class="text-blue-400">Upsert</span>(<span class="text-green-400">"prod_vectors"</span>, points)
}`,
    ts: `<span class="text-purple-400">import</span> { Vectron } <span class="text-purple-400">from</span> <span class="text-green-400">'@vectron/node'</span>;

<span class="text-purple-400">const</span> client = <span class="text-purple-400">new</span> Vectron({
    endpoint: <span class="text-green-400">'grpc.vectron.cloud:443'</span>,
    key: <span class="text-green-400">'vk_live_8x92...'</span>
});

<span class="text-neutral-500">// Fully typed responses</span>
<span class="text-purple-400">const</span> results = <span class="text-purple-400">await</span> client.query({
    collection: <span class="text-green-400">'prod_vectors'</span>,
    vector: [<span class="text-yellow-400">0.1</span>, <span class="text-yellow-400">0.2</span>, <span class="text-yellow-400">0.3</span>, <span class="text-yellow-400">0.4</span>],
    topK: <span class="text-yellow-400">5</span>
});`,
    py: `<span class="text-purple-400">from</span> vectron <span class="text-purple-400">import</span> Client

client = Client(
    endpoint=<span class="text-green-400">"grpc.vectron.cloud:443"</span>,
    api_key=<span class="text-green-400">"vk_live_8x92..."</span>
)

<span class="text-neutral-500"># Pythonic API</span>
results = client.search(
    collection=<span class="text-green-400">"prod_vectors"</span>,
    vector=[<span class="text-yellow-400">0.1</span>, <span class="text-yellow-400">0.2</span>, <span class="text-yellow-400">0.3</span>, <span class="text-yellow-400">0.4</span>],
    limit=<span class="text-yellow-400">5</span>
)`,
  };

  return (
    <section className="py-24 border-b border-white/5 bg-black/50">
      <div className="container mx-auto px-6 grid lg:grid-cols-2 gap-16 items-center">
        <div>
          <h2 className="text-3xl md:text-4xl font-bold tracking-tight text-white mb-6">
            Designed for <br />
            <span className="text-neutral-500">Developer Happiness.</span>
          </h2>
          <p className="text-lg text-neutral-400 mb-8 leading-relaxed">
            We abstracted away the complexity of Raft consensus and sharding.
            You just connect, create a collection, and start shipping.
          </p>

          <div className="space-y-4">
            <div className="flex items-start gap-4">
              <div className="p-2 bg-white/5 rounded-lg border border-white/10">
                <Terminal size={20} className="text-purple-400" />
              </div>
              <div>
                <h3 className="text-white font-medium">Type-safe SDKs</h3>
                <p className="text-sm text-neutral-500">
                  First-class support for Go, TS, and Python.
                </p>
              </div>
            </div>
            <div className="flex items-start gap-4">
              <div className="p-2 bg-white/5 rounded-lg border border-white/10">
                <Zap size={20} className="text-yellow-400" />
              </div>
              <div>
                <h3 className="text-white font-medium">Auto-Batching</h3>
                <p className="text-sm text-neutral-500">
                  Built-in high throughput ingestion pipeline.
                </p>
              </div>
            </div>
          </div>
        </div>

        {/* IDE Window */}
        <div className="rounded-xl overflow-hidden bg-[#0a0a0a] border border-white/10 shadow-2xl shadow-purple-900/10">
          <div className="flex items-center justify-between px-4 py-3 bg-[#0a0a0a] border-b border-white/10">
            <div className="flex gap-4">
              {(["go", "ts", "py"] as const).map((lang) => (
                <button
                  key={lang}
                  onClick={() => setActiveTab(lang)}
                  className={`text-xs font-medium uppercase tracking-wider transition-colors ${
                    activeTab === lang
                      ? "text-white"
                      : "text-neutral-600 hover:text-neutral-400"
                  }`}
                >
                  {lang === "ts"
                    ? "TypeScript"
                    : lang === "py"
                      ? "Python"
                      : "Go"}
                </button>
              ))}
            </div>
            <div className="flex gap-1.5">
              <div className="w-2.5 h-2.5 rounded-full bg-red-500/20" />
              <div className="w-2.5 h-2.5 rounded-full bg-yellow-500/20" />
              <div className="w-2.5 h-2.5 rounded-full bg-green-500/20" />
            </div>
          </div>
          <div className="p-6 overflow-x-auto min-h-[300px]">
            <pre className="font-mono text-sm leading-relaxed text-neutral-300">
              <code dangerouslySetInnerHTML={{ __html: codes[activeTab] }} />
            </pre>
          </div>
        </div>
      </div>
    </section>
  );
};

const SDKGrid = () => (
  <section className="py-24 bg-black">
    <div className="container mx-auto px-6 text-center max-w-4xl">
      <h2 className="text-sm font-semibold text-neutral-500 uppercase tracking-widest mb-12">
        Native SDKs available now
      </h2>
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        {[
          {
            name: "Node.js",
            icon: <Box className="w-5 h-5" />,
            status: "Stable",
          },
          {
            name: "Python",
            icon: <Terminal className="w-5 h-5" />,
            status: "Stable",
          },
          { name: "Go", icon: <Cpu className="w-5 h-5" />, status: "Stable" },
          {
            name: "Rust",
            icon: <Code className="w-5 h-5" />,
            status: "Q1 2026",
          },
        ].map((sdk) => (
          <div
            key={sdk.name}
            className="flex items-center justify-between p-4 rounded-lg border border-white/5 bg-white/5 hover:bg-white/10 transition-colors group cursor-default"
          >
            <div className="flex items-center gap-3">
              <span className="text-neutral-400 group-hover:text-white transition-colors">
                {sdk.icon}
              </span>
              <span className="font-medium text-white">{sdk.name}</span>
            </div>
            <span
              className={`text-[10px] px-2 py-0.5 rounded-full border ${
                sdk.status === "Stable"
                  ? "bg-green-500/10 text-green-400 border-green-500/20"
                  : "bg-neutral-800 text-neutral-500 border-neutral-700"
              }`}
            >
              {sdk.status}
            </span>
          </div>
        ))}
      </div>
    </div>
  </section>
);

const BentoGrid = () => (
  <section className="py-32 bg-black border-t border-white/5">
    <div className="container mx-auto px-6 max-w-6xl">
      <div className="mb-16">
        <h2 className="text-3xl font-bold text-white mb-4">
          Uncompromising Performance
        </h2>
        <p className="text-neutral-400">
          Built on a multi-Raft architecture to separate storage from compute.
        </p>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-6 lg:grid-cols-6 gap-4 auto-rows-[180px]">
        {/* Large Latency Card */}
        <SpotlightCard className="md:col-span-4 lg:col-span-4 row-span-2 p-10 flex flex-col justify-between">
          <div className="absolute top-0 right-0 p-12 opacity-5">
            <Zap className="w-64 h-64 text-white" />
          </div>
          <div>
            <h3 className="text-2xl font-bold text-white mb-2">
              Sub-millisecond Reads
            </h3>
            <p className="text-neutral-400 max-w-sm">
              Proprietary caching layer ensures that 99% of your queries return
              in under 10ms, even with billions of vectors.
            </p>
          </div>
          <div className="flex items-end gap-3 z-10">
            <span className="text-8xl font-mono font-bold text-white tracking-tighter">
              4<span className="text-neutral-600 text-4xl">ms</span>
            </span>
            <span className="text-sm text-neutral-500 mb-6 font-mono uppercase tracking-widest">
              p99 Latency
            </span>
          </div>
        </SpotlightCard>

        {/* Uptime Card */}
        <SpotlightCard className="md:col-span-2 lg:col-span-2 row-span-1 p-6 flex flex-col justify-center">
          <div className="flex items-center gap-2 mb-2 text-green-500">
            <Shield size={20} />
            <span className="text-xs font-mono uppercase">Enterprise SLA</span>
          </div>
          <div className="text-4xl font-mono font-bold text-white">99.99%</div>
          <p className="text-sm text-neutral-500 mt-1">Uptime Guarantee</p>
        </SpotlightCard>

        {/* Global Scale Card */}
        <SpotlightCard className="md:col-span-2 lg:col-span-2 row-span-1 p-6 flex flex-col justify-center">
          <div className="flex items-center gap-2 mb-2 text-blue-500">
            <Globe size={20} />
            <span className="text-xs font-mono uppercase">Distributed</span>
          </div>
          <div className="text-xl font-bold text-white">Global Replication</div>
          <p className="text-sm text-neutral-500 mt-1">
            Multi-region by default.
          </p>
        </SpotlightCard>

        {/* Architecture Details */}
        <SpotlightCard className="md:col-span-3 lg:col-span-3 row-span-1 p-6 flex items-center gap-6">
          <div className="p-3 bg-purple-500/10 rounded-lg text-purple-400">
            <Layers size={24} />
          </div>
          <div>
            <h3 className="text-lg font-bold text-white">Auto-Sharding</h3>
            <p className="text-sm text-neutral-400">
              PD manages topology automatically.
            </p>
          </div>
        </SpotlightCard>

        <SpotlightCard className="md:col-span-3 lg:col-span-3 row-span-1 p-6 flex items-center gap-6">
          <div className="p-3 bg-pink-500/10 rounded-lg text-pink-400">
            <Database size={24} />
          </div>
          <div>
            <h3 className="text-lg font-bold text-white">Raft Consensus</h3>
            <p className="text-sm text-neutral-400">
              Linearizable consistency for all writes.
            </p>
          </div>
        </SpotlightCard>
      </div>
    </div>
  </section>
);

const UseCases = () => (
  <section className="py-24 bg-black border-t border-white/5">
    <div className="container mx-auto px-6">
      <div className="text-center mb-16">
        <h2 className="text-3xl font-bold text-white mb-4">
          Build what's next
        </h2>
        <p className="text-neutral-400 max-w-xl mx-auto">
          Vectron provides the primitives you need to build intelligent,
          context-aware applications.
        </p>
      </div>

      <div className="grid md:grid-cols-3 gap-8 max-w-6xl mx-auto">
        <div className="group">
          <div className="mb-4 w-12 h-12 rounded-lg bg-neutral-900 border border-white/5 flex items-center justify-center group-hover:bg-purple-500/10 group-hover:border-purple-500/20 transition-colors">
            <Brain className="w-6 h-6 text-neutral-400 group-hover:text-purple-400" />
          </div>
          <h3 className="text-xl font-semibold text-white mb-2">
            LLM Memory (RAG)
          </h3>
          <p className="text-neutral-500 leading-relaxed mb-4">
            Give your LLMs long-term memory. Store millions of documents and
            retrieve the relevant context in milliseconds.
          </p>
          <Link
            to="/docs/rag"
            className="text-sm font-medium text-white border-b border-white/20 hover:border-white pb-0.5"
          >
            Learn more
          </Link>
        </div>

        <div className="group">
          <div className="mb-4 w-12 h-12 rounded-lg bg-neutral-900 border border-white/5 flex items-center justify-center group-hover:bg-blue-500/10 group-hover:border-blue-500/20 transition-colors">
            <Search className="w-6 h-6 text-neutral-400 group-hover:text-blue-400" />
          </div>
          <h3 className="text-xl font-semibold text-white mb-2">
            Semantic Search
          </h3>
          <p className="text-neutral-500 leading-relaxed mb-4">
            Move beyond keyword matching. Implement hybrid search that
            understands the intent behind the query.
          </p>
          <Link
            to="/docs/search"
            className="text-sm font-medium text-white border-b border-white/20 hover:border-white pb-0.5"
          >
            Learn more
          </Link>
        </div>

        <div className="group">
          <div className="mb-4 w-12 h-12 rounded-lg bg-neutral-900 border border-white/5 flex items-center justify-center group-hover:bg-pink-500/10 group-hover:border-pink-500/20 transition-colors">
            <Sparkles className="w-6 h-6 text-neutral-400 group-hover:text-pink-400" />
          </div>
          <h3 className="text-xl font-semibold text-white mb-2">
            Recommendations
          </h3>
          <p className="text-neutral-500 leading-relaxed mb-4">
            Power real-time recommendation engines for e-commerce, content
            platforms, and social feeds.
          </p>
          <Link
            to="/docs/recs"
            className="text-sm font-medium text-white border-b border-white/20 hover:border-white pb-0.5"
          >
            Learn more
          </Link>
        </div>
      </div>
    </div>
  </section>
);

const PreFooterCTA = () => (
  <section className="py-32 relative overflow-hidden text-center bg-black border-t border-white/5">
    <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-[600px] h-[300px] bg-gradient-to-b from-purple-600/10 to-transparent blur-[120px] pointer-events-none" />

    <div className="relative z-10 container mx-auto px-6">
      <h2 className="text-5xl font-bold tracking-tighter text-white mb-8">
        Ready for production?
      </h2>
      <div className="flex flex-col sm:flex-row items-center justify-center gap-4">
        <Link
          to="/signup"
          className="h-14 px-8 rounded-full bg-white text-black font-semibold text-lg flex items-center gap-2 hover:bg-neutral-200 transition-all shadow-[0_0_30px_-5px_rgba(255,255,255,0.3)]"
        >
          Get your API Key
        </Link>
        <Link
          to="/contact"
          className="h-14 px-8 rounded-full border border-white/10 text-white font-medium text-lg flex items-center gap-2 hover:bg-white/5 transition-all"
        >
          Contact Sales
        </Link>
      </div>
    </div>
  </section>
);

const Footer = () => (
  <footer className="py-12 bg-black border-t border-white/5 text-sm text-neutral-500">
    <div className="container mx-auto px-6 grid grid-cols-2 md:grid-cols-4 gap-8 mb-12">
      <div className="col-span-2 md:col-span-1">
        <div className="flex items-center gap-2 text-white font-bold mb-4">
          <div className="w-4 h-4 rounded-full bg-gradient-to-r from-purple-500 to-pink-500" />
          Vectron
        </div>
        <p className="mb-4">The database for the AI era.</p>
      </div>
      <div>
        <h4 className="text-white font-medium mb-4">Product</h4>
        <ul className="space-y-2">
          <li>
            <Link to="/features" className="hover:text-white transition-colors">
              Features
            </Link>
          </li>
          <li>
            <Link to="/pricing" className="hover:text-white transition-colors">
              Pricing
            </Link>
          </li>
          <li>
            <Link
              to="/changelog"
              className="hover:text-white transition-colors"
            >
              Changelog
            </Link>
          </li>
        </ul>
      </div>
      <div>
        <h4 className="text-white font-medium mb-4">Developers</h4>
        <ul className="space-y-2">
          <li>
            <Link to="/docs" className="hover:text-white transition-colors">
              Documentation
            </Link>
          </li>
          <li>
            <Link to="/api" className="hover:text-white transition-colors">
              API Reference
            </Link>
          </li>
          <li>
            <Link to="/status" className="hover:text-white transition-colors">
              Status
            </Link>
          </li>
        </ul>
      </div>
      <div>
        <h4 className="text-white font-medium mb-4">Company</h4>
        <ul className="space-y-2">
          <li>
            <Link to="/about" className="hover:text-white transition-colors">
              About
            </Link>
          </li>
          <li>
            <Link to="/blog" className="hover:text-white transition-colors">
              Blog
            </Link>
          </li>
          <li>
            <Link to="/careers" className="hover:text-white transition-colors">
              Careers
            </Link>
          </li>
        </ul>
      </div>
    </div>
    <div className="container mx-auto px-6 pt-8 border-t border-white/5 text-center">
      <p>&copy; 2026 Vectron Inc. All rights reserved.</p>
    </div>
  </footer>
);

export const HomePage = () => {
  const { user } = useAuth();
  const navigate = useNavigate();

  useEffect(() => {
    if (user) navigate("/dashboard");
  }, [user, navigate]);

  return (
    <div className="min-h-screen bg-black text-white font-sans selection:bg-purple-500 selection:text-white">
      <header className="fixed top-0 inset-x-0 z-50 border-b border-white/5 bg-black/50 backdrop-blur-xl">
        <div className="container mx-auto h-16 flex items-center justify-between px-6">
          <Link
            to="/"
            className="text-lg font-bold tracking-tight text-white flex items-center gap-2"
          >
            <div className="w-3 h-3 rounded-full bg-gradient-to-r from-purple-500 to-pink-600" />
            Vectron
          </Link>
          <div className="flex items-center gap-4">
            <Link
              to="/login"
              className="text-sm font-medium text-neutral-400 hover:text-white transition-colors"
            >
              Login
            </Link>
            <Link
              to="/signup"
              className="text-sm font-medium bg-white text-black px-4 py-2 rounded-full hover:bg-neutral-200 transition-colors"
            >
              Get Started
            </Link>
          </div>
        </div>
      </header>

      <main>
        <Hero />
        <TrustedBy />
        <CodeExample />
        <SDKGrid />
        <BentoGrid />
        <UseCases />
        <PreFooterCTA />
      </main>

      <Footer />
    </div>
  );
};
