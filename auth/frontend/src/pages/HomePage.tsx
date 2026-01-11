import React, { useEffect } from "react";
import { Link, useNavigate } from "react-router-dom";
import {
  Gauge,
  GitBranch,
  Lock,
  Code,
  ChevronRight,
  Terminal,
} from "lucide-react";
import { useAuth } from "../contexts/AuthContext";

// --- Components ---

const FeatureCard = ({
  icon,
  title,
  description,
}: {
  icon: React.ReactNode;
  title: string;
  description: string;
}) => (
  <div className="group relative p-6 h-full rounded-2xl border border-neutral-800 bg-neutral-900/30 hover:bg-neutral-900/50 hover:border-neutral-700 transition-all duration-300">
    <div className="absolute inset-0 bg-gradient-to-br from-purple-500/5 to-transparent rounded-2xl opacity-0 group-hover:opacity-100 transition-opacity" />
    <div className="relative z-10 flex flex-col items-start text-left">
      <div className="p-3 mb-4 rounded-lg bg-neutral-800/50 text-purple-400 group-hover:text-purple-300 group-hover:bg-purple-900/20 transition-colors">
        {icon}
      </div>
      <h3 className="text-lg font-semibold text-white mb-2">{title}</h3>
      <p className="text-sm text-neutral-400 leading-relaxed">{description}</p>
    </div>
  </div>
);

const HomePageHeader = () => {
  return (
    <header className="fixed top-0 left-0 right-0 z-50 border-b border-neutral-800 bg-black/50 backdrop-blur-xl">
      <div className="container mx-auto h-16 flex items-center justify-between px-4 sm:px-6">
        <span className="text-xl font-bold tracking-tight text-white flex items-center gap-2">
          <div className="w-3 h-3 rounded-full bg-gradient-to-r from-purple-500 to-pink-500" />
          Vectron
        </span>
        <nav className="flex items-center space-x-6">
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
            Sign Up
          </Link>
        </nav>
      </div>
    </header>
  );
};

export const HomePage = () => {
  const { user } = useAuth();
  const navigate = useNavigate();

  useEffect(() => {
    if (user) {
      navigate("/dashboard");
    }
  }, [user, navigate]);

  return (
    <div className="min-h-screen bg-black text-white selection:bg-purple-500 selection:text-white font-sans">
      <HomePageHeader />

      <main className="relative pt-16">
        {/* Ambient Glow Effects */}
        <div className="fixed top-20 left-1/2 -translate-x-1/2 w-[800px] h-[500px] bg-purple-900/20 blur-[120px] rounded-full pointer-events-none -z-10" />

        {/* Hero Section */}
        <section className="relative py-24 sm:py-32 text-center overflow-hidden">
          <div className="container mx-auto px-4 sm:px-6 max-w-5xl relative z-10">
            {/* Pill Label */}
            <div className="inline-flex items-center gap-2 px-3 py-1 rounded-full border border-neutral-800 bg-neutral-900/50 mb-8 animate-fade-in-up">
              <span className="flex h-2 w-2 rounded-full bg-green-500"></span>
              <span className="text-xs font-medium text-neutral-300">
                v1.0 is now available
              </span>
            </div>

            <h1 className="text-5xl sm:text-7xl font-bold tracking-tighter mb-6 text-white leading-tight">
              Distributed Vector Search,{" "}
              <span className="text-transparent bg-clip-text bg-gradient-to-r from-purple-400 via-pink-500 to-red-500">
                Simplified.
              </span>
            </h1>

            <p className="text-lg sm:text-xl text-neutral-400 max-w-2xl mx-auto mb-10 leading-relaxed">
              Vectron is a distributed vector database for high-performance
              similarity search. Operational simplicity meets strong
              consistency.
            </p>

            <div className="flex flex-col sm:flex-row items-center justify-center gap-4">
              <Link
                to="/signup"
                className="h-12 px-8 rounded-full bg-white text-black font-semibold flex items-center gap-2 hover:bg-neutral-200 transition-all"
              >
                Get Started
                <ChevronRight className="w-4 h-4" />
              </Link>
              <Link
                to="/docs"
                className="h-12 px-8 rounded-full border border-neutral-700 bg-neutral-900/50 text-white font-medium flex items-center hover:bg-neutral-800 transition-all"
              >
                Read Documentation
              </Link>
            </div>
          </div>
        </section>

        {/* Features Section */}
        <section className="py-24 border-t border-neutral-800 bg-black/50">
          <div className="container mx-auto px-4 sm:px-6">
            <div className="text-center mb-16">
              <h2 className="text-3xl font-bold tracking-tight mb-4">
                Built for scale
              </h2>
              <p className="text-neutral-400">
                Everything you need to build AI applications.
              </p>
            </div>
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
              <FeatureCard
                icon={<Gauge className="w-6 h-6" />}
                title="High Performance"
                description="Built for speed using state-of-the-art ANN algorithms like HNSW for blazingly fast similarity search."
              />
              <FeatureCard
                icon={<GitBranch className="w-6 h-6" />}
                title="Scalable & Resilient"
                description="Distributed architecture with automatic sharding and Raft-based replication ensures high availability."
              />
              <FeatureCard
                icon={<Lock className="w-6 h-6" />}
                title="Strongly Consistent"
                description="All reads can be linearizable, ensuring you are always working with the most up-to-date data."
              />
              <FeatureCard
                icon={<Code className="w-6 h-6" />}
                title="Developer Friendly"
                description="Idiomatic client libraries for Go, Python, and JavaScript, along with a simple RESTful API."
              />
            </div>
          </div>
        </section>

        {/* Why Vectron Section */}
        <section className="py-32 relative">
          <div className="container mx-auto px-4 sm:px-6 max-w-6xl">
            <div className="grid grid-cols-1 lg:grid-cols-2 gap-16 items-center">
              <div>
                <h2 className="text-4xl sm:text-5xl font-bold tracking-tighter mb-6">
                  A Modern, Simple Architecture
                </h2>
                <div className="space-y-6 text-lg text-neutral-400 leading-relaxed">
                  <p>
                    Vectron features a clean separation between its control
                    plane (Placement Driver) and data plane (Workers). This
                    "multi-Raft" design provides strong consistency by
                    minimizing dependencies.
                  </p>
                  <p>
                    No external Zookeeper, no message queues. Just two service
                    types to deploy for a fully fault-tolerant cluster.
                  </p>
                </div>

                <div className="mt-8 flex gap-4">
                  <div className="flex items-center gap-2 text-sm text-neutral-300">
                    <div className="w-2 h-2 bg-purple-500 rounded-full" /> Raft
                    Consensus
                  </div>
                  <div className="flex items-center gap-2 text-sm text-neutral-300">
                    <div className="w-2 h-2 bg-pink-500 rounded-full" /> Zero
                    Dependencies
                  </div>
                </div>
              </div>

              {/* Code Window */}
              <div className="relative group">
                <div className="absolute -inset-1 bg-gradient-to-r from-purple-600 to-pink-600 rounded-xl blur opacity-25 group-hover:opacity-50 transition duration-1000"></div>
                <div className="relative rounded-xl bg-[#0a0a0a] border border-neutral-800 shadow-2xl overflow-hidden">
                  <div className="flex items-center px-4 py-3 border-b border-neutral-800 bg-neutral-900/50">
                    <div className="flex space-x-2">
                      <div className="w-3 h-3 rounded-full bg-red-500/20 border border-red-500/50" />
                      <div className="w-3 h-3 rounded-full bg-yellow-500/20 border border-yellow-500/50" />
                      <div className="w-3 h-3 rounded-full bg-green-500/20 border border-green-500/50" />
                    </div>
                    <div className="ml-4 text-xs text-neutral-500 font-mono flex items-center gap-2">
                      <Terminal className="w-3 h-3" /> main.py
                    </div>
                  </div>
                  <div className="p-6 overflow-x-auto">
                    <pre className="font-mono text-sm leading-relaxed">
                      <code className="text-neutral-300">
                        {`import vectron_client

# Initialize the client
client = vectron_client.Client(
    api_key="sk_live_..."
)

# Get your collection
collection = client.get_collection("production")

# Upsert vectors with metadata
collection.upsert([
    {"id": "doc1", "vector": [0.1, ...]},
    {"id": "doc2", "vector": [0.4, ...]},
])`}
                      </code>
                    </pre>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </section>

        {/* Final CTA Section */}
        <section className="py-24 border-t border-neutral-800 text-center relative overflow-hidden">
          <div className="absolute top-0 left-1/2 -translate-x-1/2 w-[600px] h-[300px] bg-gradient-to-b from-purple-900/10 to-transparent blur-[80px] pointer-events-none" />

          <div className="container mx-auto px-4 sm:px-6 max-w-3xl relative z-10">
            <h2 className="text-4xl font-bold tracking-tighter mb-6 text-white">
              Ready to ship?
            </h2>
            <p className="text-lg text-neutral-400 mb-10">
              Create an account today and get your API key in minutes. No credit
              card required for the free tier.
            </p>
            <Link
              to="/signup"
              className="inline-flex h-12 px-8 items-center justify-center rounded-full bg-white text-black font-semibold hover:bg-neutral-200 transition-all shadow-[0_0_20px_-5px_rgba(255,255,255,0.3)]"
            >
              Start Building for Free
            </Link>
          </div>
        </section>
      </main>

      {/* Footer */}
      <footer className="py-12 border-t border-neutral-800 text-center text-sm text-neutral-600 bg-black">
        <div className="container mx-auto px-4 sm:px-6 flex flex-col items-center gap-4">
          <div className="flex items-center gap-2 text-neutral-400 font-semibold">
            <div className="w-4 h-4 rounded-full bg-gradient-to-r from-purple-500 to-pink-500" />
            Vectron
          </div>
          <p>
            &copy; {new Date().getFullYear()} Vectron Inc. All rights reserved.
          </p>
        </div>
      </footer>
    </div>
  );
};
