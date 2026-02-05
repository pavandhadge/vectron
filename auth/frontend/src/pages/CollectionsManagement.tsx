import { useState, useEffect } from "react";
import {
  Database,
  Plus,
  Search,
  RefreshCw,
  Trash2,
  Eye,
  EyeOff,
  CheckCircle,
  XCircle,
  AlertTriangle,
  Clock,
  X,
  Layers,
} from "lucide-react";
import { Collection } from "../api-types";
import { managementApi } from "../services/managementApi";
import {
  formatBytes,
  formatDateTime,
  formatNumber,
  toLowerSafe,
  toNumber,
} from "../utils/format";



interface CreateCollectionModalProps {
  onClose: () => void;
  onCreated: () => void;
}

interface CollectionDetailsModalProps {
  collection: Collection;
  onClose: () => void;
}

interface DeleteConfirmModalProps {
  collection: Collection;
  onClose: () => void;
  onConfirm: () => void;
  error: string | null;
}

const CreateCollectionModal = ({ onClose, onCreated }: CreateCollectionModalProps) => {
  const [name, setName] = useState("");
  const [dimension, setDimension] = useState(1536);
  const [distance, setDistance] = useState("cosine");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;

    setLoading(true);
    setError(null);

    try {
      await managementApi.createCollection(name.trim(), dimension, distance);
      onCreated();
      onClose();
    } catch (err) {
      setError("Failed to create collection");
      console.error("Error creating collection:", err);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
      <div className="bg-neutral-900 border border-neutral-800 rounded-xl max-w-md w-full">
        <div className="flex items-center justify-between p-6 border-b border-neutral-800">
          <h2 className="text-xl font-semibold text-white">Create Collection</h2>
          <button
            onClick={onClose}
            className="p-2 hover:bg-neutral-800 rounded-lg transition-colors"
          >
            <X className="w-5 h-5 text-neutral-400" />
          </button>
        </div>

        <form onSubmit={handleSubmit} className="p-6 space-y-4">
          {error && (
            <div className="p-3 bg-red-500/10 border border-red-500/20 rounded-lg text-red-400 text-sm">
              {error}
            </div>
          )}

          <div>
            <label className="block text-sm font-medium text-white mb-2">
              Collection Name
            </label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="w-full bg-neutral-800 border border-neutral-700 rounded-lg px-3 py-2 text-white placeholder-neutral-400 focus:ring-2 focus:ring-purple-500 focus:border-transparent"
              placeholder="Enter collection name"
              required
            />
          </div>

          <div>
            <label className="block text-sm font-medium text-white mb-2">
              Vector Dimension
            </label>
            <input
              type="number"
              value={dimension}
              onChange={(e) => setDimension(parseInt(e.target.value) || 0)}
              className="w-full bg-neutral-800 border border-neutral-700 rounded-lg px-3 py-2 text-white placeholder-neutral-400 focus:ring-2 focus:ring-purple-500 focus:border-transparent"
              min="1"
              required
            />
          </div>

          <div>
            <label className="block text-sm font-medium text-white mb-2">
              Distance Metric
            </label>
            <select
              value={distance}
              onChange={(e) => setDistance(e.target.value)}
              className="w-full bg-neutral-800 border border-neutral-700 rounded-lg px-3 py-2 text-white focus:ring-2 focus:ring-purple-500 focus:border-transparent"
            >
              <option value="cosine">Cosine</option>
              <option value="euclidean">Euclidean</option>
              <option value="dot">Dot Product</option>
            </select>
          </div>

          <div className="flex gap-3 pt-4">
            <button
              type="button"
              onClick={onClose}
              className="flex-1 px-4 py-2 bg-neutral-800 text-white rounded-lg hover:bg-neutral-700 transition-colors"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={loading || !name.trim()}
              className="flex-1 px-4 py-2 bg-purple-600 text-white rounded-lg hover:bg-purple-700 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
            >
              {loading ? "Creating..." : "Create Collection"}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
};

const CollectionDetailsModal = ({ collection, onClose }: CollectionDetailsModalProps) => {
  const collectionName = collection.name || "Unnamed Collection";

  const getStatusIcon = (status: string) => {
    switch (status) {
      case "healthy":
      case "active":
        return <CheckCircle className="w-4 h-4 text-green-400" />;
      case "degraded":
        return <AlertTriangle className="w-4 h-4 text-yellow-400" />;
      case "error":
        return <XCircle className="w-4 h-4 text-red-400" />;
      default:
        return <Clock className="w-4 h-4 text-neutral-400" />;
    }
  };

  const getStatusColor = (status: string) => {
    switch (status) {
      case "healthy":
      case "active":
        return "text-green-400";
      case "degraded":
        return "text-yellow-400";
      case "error":
        return "text-red-400";
      default:
        return "text-neutral-400";
    }
  };

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
      <div className="bg-neutral-900 border border-neutral-800 rounded-xl max-w-4xl w-full max-h-[90vh] overflow-y-auto">
        <div className="flex items-center justify-between p-6 border-b border-neutral-800">
          <h2 className="text-xl font-semibold text-white">Collection Details: {collectionName}</h2>
          <button
            onClick={onClose}
            className="p-2 hover:bg-neutral-800 rounded-lg transition-colors"
          >
            <X className="w-5 h-5 text-neutral-400" />
          </button>
        </div>

        <div className="p-6 space-y-6">
          {/* Basic Info */}
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
            <div className="space-y-4">
              <h3 className="text-lg font-semibold text-white">Collection Information</h3>
              <div className="space-y-3">
                <div className="flex justify-between">
                  <span className="text-neutral-400">Name:</span>
                  <span className="text-white font-semibold">{collectionName}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-neutral-400">Dimension:</span>
                  <span className="text-white font-mono">{collection.dimension}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-neutral-400">Distance Metric:</span>
                  <span className="text-white font-mono">{collection.distance}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-neutral-400">Status:</span>
                  <div className="flex items-center gap-2">
                    {getStatusIcon(collection.status)}
                    <span className={getStatusColor(collection.status)}>
                      {collection.status.charAt(0).toUpperCase() + collection.status.slice(1)}
                    </span>
                  </div>
                </div>
                <div className="flex justify-between">
                  <span className="text-neutral-400">Created:</span>
                  <span className="text-white">
                    {formatDateTime(collection.created_at)}
                  </span>
                </div>
              </div>
            </div>

            <div className="space-y-4">
              <h3 className="text-lg font-semibold text-white">Statistics</h3>
              <div className="space-y-3">
                <div className="flex justify-between">
                  <span className="text-neutral-400">Vector Count:</span>
                  <span className="text-white font-semibold">
                    {formatNumber(collection.vector_count)}
                  </span>
                </div>
                <div className="flex justify-between">
                  <span className="text-neutral-400">Storage Size:</span>
                  <span className="text-white font-semibold">
                    {formatBytes(collection.size_bytes)}
                  </span>
                </div>
                <div className="flex justify-between">
                  <span className="text-neutral-400">Shard Count:</span>
                  <span className="text-white font-semibold">
                    {collection.shard_count}
                  </span>
                </div>
                <div className="flex justify-between">
                  <span className="text-neutral-400">Avg Vectors/Shard:</span>
                  <span className="text-white font-semibold">
                    {collection.shard_count > 0
                      ? formatNumber(
                          Math.round(
                            toNumber(collection.vector_count) /
                              Math.max(collection.shard_count, 1),
                          ),
                        )
                      : "0"}
                  </span>
                </div>
              </div>
            </div>
          </div>

          {/* Shard Details */}
          {collection.shards && collection.shards.length > 0 && (
            <div className="space-y-4">
              <h3 className="text-lg font-semibold text-white">Shard Details</h3>
              <div className="overflow-x-auto">
                <table className="w-full border border-neutral-800 rounded-lg">
                  <thead className="bg-neutral-800/50">
                    <tr>
                      <th className="text-left p-3 text-neutral-400 font-medium">Shard ID</th>
                      <th className="text-left p-3 text-neutral-400 font-medium">Leader</th>
                      <th className="text-left p-3 text-neutral-400 font-medium">Workers</th>
                      <th className="text-right p-3 text-neutral-400 font-medium">Vectors</th>
                      <th className="text-right p-3 text-neutral-400 font-medium">Size</th>
                      <th className="text-center p-3 text-neutral-400 font-medium">Status</th>
                      <th className="text-right p-3 text-neutral-400 font-medium">Last Updated</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-neutral-800">
                    {collection.shards.map((shard) => (
                      <tr key={shard.shard_id}>
                        <td className="p-3 text-white font-mono">{shard.shard_id}</td>
                        <td className="p-3 text-white font-mono">{shard.leader_id}</td>
                        <td className="p-3">
                          <div className="flex flex-wrap gap-1">
                            {shard.worker_ids.map((workerId) => (
                              <span
                                key={workerId}
                                className="px-2 py-1 bg-blue-500/10 text-blue-400 rounded text-xs font-mono"
                              >
                                {workerId}
                              </span>
                            ))}
                          </div>
                        </td>
                        <td className="p-3 text-right text-white">
                          {formatNumber(shard.vector_count)}
                        </td>
                        <td className="p-3 text-right text-white">
                          {formatBytes(shard.size_bytes)}
                        </td>
                        <td className="p-3 text-center">
                          <span className={`px-2 py-1 rounded text-xs ${
                            shard.status === "healthy" ? "bg-green-500/10 text-green-400" :
                            shard.status === "degraded" ? "bg-yellow-500/10 text-yellow-400" :
                            "bg-red-500/10 text-red-400"
                          }`}>
                            {shard.status}
                          </span>
                        </td>
                        <td className="p-3 text-right text-white text-sm">
                          {formatDateTime(shard.last_updated)}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
};

const DeleteConfirmModal = ({ collection, onClose, onConfirm, error }: DeleteConfirmModalProps) => {
  const [confirmText, setConfirmText] = useState("");
  const [loading, setLoading] = useState(false);

  const handleConfirm = async () => {
    if (confirmText !== collection.name) return;
    
    setLoading(true);
    try {
      await onConfirm();
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
      <div className="bg-neutral-900 border border-neutral-800 rounded-xl max-w-md w-full">
        <div className="flex items-center justify-between p-6 border-b border-neutral-800">
          <h2 className="text-xl font-semibold text-white">Delete Collection</h2>
          <button
            onClick={onClose}
            className="p-2 hover:bg-neutral-800 rounded-lg transition-colors"
          >
            <X className="w-5 h-5 text-neutral-400" />
          </button>
        </div>

        <div className="p-6 space-y-4">
          {error && (
            <div className="flex items-center gap-3 p-4 bg-red-500/10 border border-red-500/20 rounded-lg">
              <XCircle className="w-6 h-6 text-red-400 flex-shrink-0" />
              <div>
                <h3 className="text-red-400 font-semibold">Error</h3>
                <p className="text-red-300 text-sm mt-1">{error}</p>
              </div>
            </div>
          )}
          <div className="flex items-center gap-3 p-4 bg-red-500/10 border border-red-500/20 rounded-lg">
            <AlertTriangle className="w-6 h-6 text-red-400 flex-shrink-0" />
            <div>
              <h3 className="text-red-400 font-semibold">Warning: Permanent Action</h3>
              <p className="text-red-300 text-sm mt-1">
                This will permanently delete the collection and all its vectors. This action cannot be undone.
              </p>
            </div>
          </div>

          <div>
            <p className="text-white mb-2">
              You are about to delete: <strong>{collection.name}</strong>
            </p>
            <p className="text-neutral-400 text-sm mb-4">
              Type the collection name to confirm deletion:
            </p>
            <input
              type="text"
              value={confirmText}
              onChange={(e) => setConfirmText(e.target.value)}
              className="w-full bg-neutral-800 border border-neutral-700 rounded-lg px-3 py-2 text-white placeholder-neutral-400 focus:ring-2 focus:ring-red-500 focus:border-transparent"
              placeholder={collection.name}
            />
          </div>

          <div className="flex gap-3 pt-4">
            <button
              onClick={onClose}
              className="flex-1 px-4 py-2 bg-neutral-800 text-white rounded-lg hover:bg-neutral-700 transition-colors"
            >
              Cancel
            </button>
            <button
              onClick={handleConfirm}
              disabled={confirmText !== collection.name || loading}
              className="flex-1 px-4 py-2 bg-red-600 text-white rounded-lg hover:bg-red-700 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
            >
              {loading ? "Deleting..." : "Delete Collection"}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
};

const CollectionsManagement = () => {
  const [collections, setCollections] = useState<Collection[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState("");
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [selectedCollection, setSelectedCollection] = useState<Collection | null>(null);
  const [collectionToDelete, setCollectionToDelete] = useState<Collection | null>(null);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  const fetchCollections = async () => {
    try {
      setError(null);
      const collectionsData = await managementApi.getCollections();
      setCollections(collectionsData);
    } catch (err) {
      setError("Failed to fetch collections");
      console.error("Error fetching collections:", err);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchCollections();
  }, []);

  useEffect(() => {
    if (!autoRefresh) return;
    
    const interval = setInterval(() => {
      fetchCollections();
    }, 30000); // Refresh every 30 seconds

    return () => clearInterval(interval);
  }, [autoRefresh]);

  const handleDeleteCollection = async () => {
    if (!collectionToDelete) return;

    setDeleteError(null);
    try {
      await managementApi.deleteCollection(collectionToDelete.name);
      await fetchCollections(); // Refresh the list
      setCollectionToDelete(null);
      setDeleteError(null);
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : "Failed to delete collection. Please try again.";
      setDeleteError(errorMessage);
      console.error("Error deleting collection:", err);
    }
  };

  const getStatusIcon = (status: string) => {
    switch (status) {
      case "active":
        return <CheckCircle className="w-5 h-5 text-green-400" />;
      case "creating":
        return <Clock className="w-5 h-5 text-yellow-400" />;
      case "error":
        return <XCircle className="w-5 h-5 text-red-400" />;
      default:
        return <Clock className="w-5 h-5 text-neutral-400" />;
    }
  };


  const filteredCollections = collections.filter(collection =>
    toLowerSafe(collection.name).includes(searchQuery.toLowerCase())
  );

  // Calculate summary statistics
  const totalVectors = collections.reduce(
    (sum, c) => sum + toNumber(c.vector_count),
    0,
  );
  const totalSize = collections.reduce(
    (sum, c) => sum + toNumber(c.size_bytes),
    0,
  );
  const activeCollections = collections.filter(c => c.status === "active").length;

  if (loading) {
    return (
      <div className="max-w-7xl mx-auto space-y-8 animate-fade-in">
        <div className="flex items-center justify-center py-12">
          <RefreshCw className="w-8 h-8 text-purple-400 animate-spin" />
        </div>
      </div>
    );
  }

  return (
    <div className="max-w-7xl mx-auto space-y-8 animate-fade-in">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-3xl font-bold tracking-tight text-white mb-2">
            Collections Management
          </h1>
          <p className="text-neutral-400">
            Manage vector collections and monitor their performance across your deployment.
          </p>
        </div>
        
        <div className="flex items-center gap-3">
          <button
            onClick={() => setAutoRefresh(!autoRefresh)}
            className={`flex items-center gap-2 px-4 py-2 rounded-lg font-medium transition-colors ${
              autoRefresh
                ? "bg-purple-600 text-white hover:bg-purple-700"
                : "bg-neutral-800 text-neutral-300 hover:bg-neutral-700"
            }`}
          >
            {autoRefresh ? <Eye className="w-4 h-4" /> : <EyeOff className="w-4 h-4" />}
            Auto Refresh
          </button>
          
          <button
            onClick={fetchCollections}
            className="flex items-center gap-2 px-4 py-2 bg-neutral-800 text-white rounded-lg hover:bg-neutral-700 transition-colors"
          >
            <RefreshCw className="w-4 h-4" />
            Refresh
          </button>
          
          <button
            onClick={() => setShowCreateModal(true)}
            className="flex items-center gap-2 px-4 py-2 bg-purple-600 text-white rounded-lg hover:bg-purple-700 transition-colors"
          >
            <Plus className="w-4 h-4" />
            Create Collection
          </button>
        </div>
      </div>

      {/* Summary Statistics */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
        <div className="p-6 rounded-xl border border-neutral-800 bg-neutral-900/30">
          <div className="flex items-center gap-3 mb-4">
            <Database className="w-5 h-5 text-blue-400" />
            <h3 className="font-medium text-white">Total Collections</h3>
          </div>
          <div className="text-2xl font-bold text-white">{collections.length}</div>
          <p className="text-sm text-neutral-400">Vector Collections</p>
        </div>

        <div className="p-6 rounded-xl border border-neutral-800 bg-neutral-900/30">
          <div className="flex items-center gap-3 mb-4">
            <CheckCircle className="w-5 h-5 text-green-400" />
            <h3 className="font-medium text-white">Active Collections</h3>
          </div>
          <div className="text-2xl font-bold text-white">
            {activeCollections}/{collections.length}
          </div>
          <p className="text-sm text-neutral-400">Ready for Queries</p>
        </div>

        <div className="p-6 rounded-xl border border-neutral-800 bg-neutral-900/30">
          <div className="flex items-center gap-3 mb-4">
            <Layers className="w-5 h-5 text-purple-400" />
            <h3 className="font-medium text-white">Total Vectors</h3>
          </div>
          <div className="text-2xl font-bold text-white">
            {formatNumber(totalVectors, "0")}
          </div>
          <p className="text-sm text-neutral-400">Across All Collections</p>
        </div>

        <div className="p-6 rounded-xl border border-neutral-800 bg-neutral-900/30">
          <div className="flex items-center gap-3 mb-4">
            <Database className="w-5 h-5 text-orange-400" />
            <h3 className="font-medium text-white">Storage Used</h3>
          </div>
          <div className="text-2xl font-bold text-white">
            {formatBytes(totalSize)}
          </div>
          <p className="text-sm text-neutral-400">Total Storage</p>
        </div>
      </div>

      {/* Search */}
      <div className="flex items-center gap-4">
        <div className="relative flex-1 max-w-md">
          <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 w-4 h-4 text-neutral-400" />
          <input
            type="text"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="w-full bg-neutral-800 border border-neutral-700 rounded-lg pl-10 pr-3 py-2 text-white placeholder-neutral-400 focus:ring-2 focus:ring-purple-500 focus:border-transparent"
            placeholder="Search collections..."
          />
        </div>
      </div>

      {/* Collections Grid */}
      {error && (
        <div className="text-center py-12">
          <AlertTriangle className="w-12 h-12 text-red-400 mx-auto mb-4" />
          <h3 className="text-lg font-medium text-white mb-2">Failed to Load Collections</h3>
          <p className="text-neutral-400 mb-4">{error}</p>
          <button
            onClick={fetchCollections}
            className="px-4 py-2 bg-purple-600 text-white rounded-lg hover:bg-purple-700 transition-colors"
          >
            Try Again
          </button>
        </div>
      )}

      {!error && filteredCollections.length === 0 && (
        <div className="text-center py-12">
          <Database className="w-12 h-12 text-neutral-500 mx-auto mb-4" />
          <h3 className="text-lg font-medium text-white mb-2">
            {collections.length === 0 ? "No Collections Found" : "No Matching Collections"}
          </h3>
          <p className="text-neutral-400 mb-4">
            {collections.length === 0 
              ? "Create your first vector collection to get started"
              : "Try adjusting your search query"}
          </p>
          {collections.length === 0 && (
            <button
              onClick={() => setShowCreateModal(true)}
              className="px-4 py-2 bg-purple-600 text-white rounded-lg hover:bg-purple-700 transition-colors"
            >
              Create Collection
            </button>
          )}
        </div>
      )}

      {!error && filteredCollections.length > 0 && (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
          {filteredCollections.map((collection, index) => (
            <div
              key={collection.name || `collection-${index}`}
              className="p-6 rounded-xl border border-neutral-800 bg-neutral-900/30 hover:bg-neutral-900/50 transition-all"
            >
              <div className="flex items-start justify-between mb-4">
                <div className="flex items-center gap-3">
                  <div className={`p-2 rounded-lg ${
                    collection.status === "active" ? "bg-green-500/10" :
                    collection.status === "creating" ? "bg-yellow-500/10" :
                    "bg-red-500/10"
                  }`}>
                    {getStatusIcon(collection.status)}
                  </div>
                  <div>
                    <h3 className="font-semibold text-white">
                      {collection.name || "Unnamed Collection"}
                    </h3>
                    <p className="text-sm text-neutral-400">
                      {collection.dimension}D • {collection.distance}
                    </p>
                  </div>
                </div>
                
                <span className={`px-2 py-1 rounded text-xs font-medium ${
                  collection.status === "active" ? "bg-green-500/10 text-green-400" :
                  collection.status === "creating" ? "bg-yellow-500/10 text-yellow-400" :
                  "bg-red-500/10 text-red-400"
                }`}>
                  {collection.status.toUpperCase()}
                </span>
              </div>

              <div className="space-y-3 mb-4">
                <div className="flex justify-between text-sm">
                  <span className="text-neutral-400">Vectors</span>
                  <span className="text-white font-medium">
                    {formatNumber(collection.vector_count)}
                  </span>
                </div>
                
                <div className="flex justify-between text-sm">
                  <span className="text-neutral-400">Shards</span>
                  <span className="text-white font-medium">
                    {collection.shard_count}
                  </span>
                </div>
                
                <div className="flex justify-between text-sm">
                  <span className="text-neutral-400">Size</span>
                  <span className="text-white font-medium">
                    {formatBytes(collection.size_bytes)}
                  </span>
                </div>
                
                <div className="flex justify-between text-sm">
                  <span className="text-neutral-400">Created</span>
                  <span className="text-white font-medium">
                    {formatDateTime(collection.created_at)}
                  </span>
                </div>
              </div>

              <div className="flex gap-2">
                <button
                  onClick={() => setSelectedCollection(collection)}
                  className="flex-1 px-3 py-2 bg-neutral-800 text-white rounded-lg hover:bg-neutral-700 transition-colors text-sm"
                >
                  View Details
                </button>
                <button
                  onClick={() => setCollectionToDelete(collection)}
                  className="px-3 py-2 bg-red-600 text-white rounded-lg hover:bg-red-700 transition-colors"
                >
                  <Trash2 className="w-4 h-4" />
                </button>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Modals */}
      {showCreateModal && (
        <CreateCollectionModal
          onClose={() => setShowCreateModal(false)}
          onCreated={() => {
            fetchCollections();
            setShowCreateModal(false);
          }}
        />
      )}

      {selectedCollection && (
        <CollectionDetailsModal
          collection={selectedCollection}
          onClose={() => setSelectedCollection(null)}
        />
      )}

      {collectionToDelete && (
        <DeleteConfirmModal
          collection={collectionToDelete}
          onClose={() => {
            setCollectionToDelete(null);
            setDeleteError(null);
          }}
          onConfirm={handleDeleteCollection}
          error={deleteError}
        />
      )}
    </div>
  );
};

export { CollectionsManagement };
export default CollectionsManagement;
