import React, { useState, useEffect, useCallback } from "react";
import { useAuth } from "../contexts/AuthContext";
import { collectionsData } from "./collectionsPage.data";
import {
  Database,
  Plus,
  Search,
  Layers,
  RefreshCw,
  Trash2,
  Cpu,
  Loader2,
} from "lucide-react";
import { Dialog } from "../components/Dialog";
import { Toast } from "../components/Toast";

interface Collection {
  name: string;
  dimension: number;
  status?: "ready" | "indexing";
  count?: number;
}

const CollectionsPage: React.FC = () => {
  const { apiGatewayApiClient } = useAuth();
  const [collections, setCollections] = useState<Collection[]>([]);

  // UI States
  const [isLoading, setIsLoading] = useState(false);
  const [isActionLoading, setIsActionLoading] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");
  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [toast, setToast] = useState<{
    msg: string;
    type: "success" | "danger";
  } | null>(null);

  // Form States
  const [newCollectionName, setNewCollectionName] = useState("");
  const [newCollectionDimension, setNewCollectionDimension] = useState<
    number | string
  >(1536);

  // Wrap in useCallback to safely use in useEffect
  const fetchCollections = useCallback(async () => {
    setIsLoading(true);
    try {
      const response = await apiGatewayApiClient.get("/v1/collections");
      const data = response.data;

      const mapped = (data.collections || []).map((c: any) => ({
        ...c,
        status: c.status || "ready",
        count: c.count || 0,
      }));
      setCollections(mapped);
    } catch (err: any) {
      const msg = err.response?.data?.message || "Failed to load collections";
      setToast({ msg, type: "danger" });
    } finally {
      setIsLoading(false);
    }
  }, [apiGatewayApiClient]);

  useEffect(() => {
    fetchCollections();
  }, [fetchCollections]);

  const handleCreateCollection = async () => {
    if (!newCollectionName.trim()) return;

    setIsActionLoading(true);
    try {
      await apiGatewayApiClient.post("/v1/collections", {
        name: newCollectionName,
        dimension: Number(newCollectionDimension),
      });

      setToast({ msg: "Collection created successfully", type: "success" });
      await fetchCollections();

      // Reset Form
      setIsCreateOpen(false);
      setNewCollectionName("");
      setNewCollectionDimension(1536);
    } catch (err: any) {
      const msg = err.response?.data?.message || "Failed to create collection";
      setToast({ msg, type: "danger" });
    } finally {
      setIsActionLoading(false);
    }
  };

  const handleDelete = async (name: string) => {
    if (!confirm(`Are you sure you want to delete ${name}?`)) return;
    try {
      await apiGatewayApiClient.delete(`/v1/collections/${name}`);

      // Optimistic update
      setCollections((prev) => prev.filter((c) => c.name !== name));
      setToast({ msg: "Collection deleted", type: "success" });
    } catch (err: any) {
      const msg = err.response?.data?.message || "Failed to delete collection";
      setToast({ msg, type: "danger" });
    }
  };

  const filteredCollections = collections.filter((c) =>
    c.name.toLowerCase().includes(searchQuery.toLowerCase()),
  );

  return (
    <div className="space-y-6 animate-fade-in">
      {/* Header */}
      <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-white">
            {collectionsData.title}
          </h1>
          <p className="text-neutral-400 mt-1 text-sm">
            {collectionsData.subtitle}
          </p>
        </div>
        <div className="flex items-center gap-3">
          <button
            onClick={fetchCollections}
            className="p-2 text-neutral-400 hover:text-white transition-colors"
            title="Refresh"
          >
            <RefreshCw
              className={`w-4 h-4 ${isLoading ? "animate-spin" : ""}`}
            />
          </button>
          <button
            onClick={() => setIsCreateOpen(true)}
            className="flex items-center gap-2 bg-white text-black px-4 py-2 rounded-md font-medium text-sm hover:bg-neutral-200 transition-all shadow-[0_0_15px_-3px_rgba(255,255,255,0.3)]"
          >
            <Plus className="w-4 h-4" />
            {collectionsData.createButtonText}
          </button>
        </div>
      </div>

      {/* Toolbar */}
      <div className="flex items-center gap-4">
        <div className="relative flex-1 max-w-md">
          <Search className="absolute left-3 top-2.5 w-4 h-4 text-neutral-500" />
          <input
            type="text"
            placeholder={collectionsData.searchPlaceholder}
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="w-full bg-[#0a0a0a] border border-neutral-800 rounded-lg pl-9 pr-4 py-2 text-sm text-white placeholder-neutral-500 focus:outline-none focus:border-neutral-600 transition-colors"
          />
        </div>
      </div>

      {/* Table */}
      <div className="rounded-xl border border-neutral-800 bg-[#0a0a0a] overflow-hidden shadow-sm">
        <div className="overflow-x-auto">
          <table className="min-w-full text-left">
            <thead>
              <tr className="border-b border-neutral-800 bg-neutral-900/30">
                <th className="px-6 py-3 text-xs font-semibold text-neutral-500 uppercase tracking-wider">
                  {collectionsData.nameColumnHeader}
                </th>
                <th className="px-6 py-3 text-xs font-semibold text-neutral-500 uppercase tracking-wider">
                  {collectionsData.statusColumnHeader}
                </th>
                <th className="px-6 py-3 text-xs font-semibold text-neutral-500 uppercase tracking-wider">
                  {collectionsData.dimensionColumnHeader}
                </th>
                <th className="px-6 py-3 text-xs font-semibold text-neutral-500 uppercase tracking-wider">
                  {collectionsData.metricColumnHeader}
                </th>
                <th className="px-6 py-3 text-right text-xs font-semibold text-neutral-500 uppercase tracking-wider">
                  {collectionsData.actionsColumnHeader}
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-neutral-800">
              {isLoading && collections.length === 0 ? (
                <tr>
                  <td
                    colSpan={5}
                    className="px-6 py-12 text-center text-neutral-500"
                  >
                    <Loader2 className="w-6 h-6 animate-spin mx-auto mb-2" />
                    Loading...
                  </td>
                </tr>
              ) : filteredCollections.length > 0 ? (
                filteredCollections.map((col) => (
                  <tr
                    key={col.name}
                    className="group hover:bg-neutral-900/40 transition-colors"
                  >
                    <td className="px-6 py-4">
                      <div className="flex items-center gap-3">
                        <div className="p-2 rounded bg-neutral-800/50 text-purple-400">
                          <Database className="w-4 h-4" />
                        </div>
                        <div>
                          <div className="font-medium text-white text-sm">
                            {col.name}
                          </div>
                          <div className="text-xs text-neutral-500">
                            {col.count?.toLocaleString()} vectors
                          </div>
                        </div>
                      </div>
                    </td>
                    <td className="px-6 py-4">
                      <span className="inline-flex items-center gap-1.5 px-2 py-1 rounded-full bg-green-500/10 text-green-400 text-xs font-medium border border-green-500/20">
                        <span className="w-1.5 h-1.5 rounded-full bg-green-500" />
                        Ready
                      </span>
                    </td>
                    <td className="px-6 py-4">
                      <div className="flex items-center gap-2 text-sm text-neutral-300">
                        <Layers className="w-4 h-4 text-neutral-600" />
                        <span className="font-mono text-xs">
                          {col.dimension}
                        </span>
                      </div>
                    </td>
                    <td className="px-6 py-4">
                      <span className="text-sm text-neutral-500 font-mono">
                        Cosine
                      </span>
                    </td>
                    <td className="px-6 py-4 text-right">
                      <button
                        onClick={() => handleDelete(col.name)}
                        className="p-2 text-neutral-500 hover:text-red-400 hover:bg-red-900/10 rounded transition-colors opacity-0 group-hover:opacity-100"
                      >
                        <Trash2 className="w-4 h-4" />
                      </button>
                    </td>
                  </tr>
                ))
              ) : (
                <tr>
                  <td colSpan={5} className="px-6 py-16 text-center">
                    <div className="flex flex-col items-center justify-center text-neutral-500">
                      <div className="w-12 h-12 bg-neutral-900 rounded-full flex items-center justify-center mb-4">
                        <Database className="w-6 h-6 text-neutral-600" />
                      </div>
                      <h3 className="text-white font-medium mb-1">
                        {collectionsData.emptyStateTitle}
                      </h3>
                      <p className="text-sm max-w-sm mb-6">
                        {collectionsData.emptyStateDesc}
                      </p>
                      <button
                        onClick={() => setIsCreateOpen(true)}
                        className="text-sm text-purple-400 hover:text-purple-300 font-medium"
                      >
                        {collectionsData.createButtonText}
                      </button>
                    </div>
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

      {/* Create Dialog */}
      <Dialog
        open={isCreateOpen}
        onClose={() => setIsCreateOpen(false)}
        title={collectionsData.createCollectionTitle}
        actions={
          <>
            <button
              onClick={() => setIsCreateOpen(false)}
              className="px-4 py-2 text-sm text-neutral-400 hover:text-white transition-colors"
            >
              Cancel
            </button>
            <button
              onClick={handleCreateCollection}
              disabled={isActionLoading}
              className="px-4 py-2 bg-white text-black text-sm font-medium rounded hover:bg-neutral-200 transition-colors disabled:opacity-50 flex items-center gap-2"
            >
              {isActionLoading && <Loader2 className="w-3 h-3 animate-spin" />}
              {isActionLoading
                ? collectionsData.creatingButtonText
                : collectionsData.createButtonText}
            </button>
          </>
        }
      >
        <div className="space-y-6 pt-2">
          <div>
            <label className="block text-xs font-medium text-neutral-400 uppercase mb-2">
              {collectionsData.collectionNameLabel}
            </label>
            <input
              type="text"
              className="w-full bg-[#050505] border border-neutral-800 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-white focus:ring-1 focus:ring-white transition-all"
              placeholder="e.g. documentation-embeddings"
              value={newCollectionName}
              onChange={(e) => setNewCollectionName(e.target.value)}
            />
          </div>

          <div>
            <label className="block text-xs font-medium text-neutral-400 uppercase mb-2">
              {collectionsData.dimensionLabel}
            </label>
            <input
              type="number"
              className="w-full bg-[#050505] border border-neutral-800 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-white focus:ring-1 focus:ring-white transition-all font-mono"
              placeholder="1536"
              value={newCollectionDimension}
              onChange={(e) => setNewCollectionDimension(e.target.value)}
            />
            <div className="mt-2 p-3 rounded bg-blue-900/10 border border-blue-900/20 flex gap-2">
              <Cpu className="w-4 h-4 text-blue-400 shrink-0 mt-0.5" />
              <p className="text-xs text-blue-300">
                {collectionsData.dimensionHelpText}
              </p>
            </div>
          </div>
        </div>
      </Dialog>

      {/* Toast */}
      {toast && (
        <Toast
          message={toast.msg}
          type={toast.type === "danger" ? "error" : toast.type}
          onClose={() => setToast(null)}
        />
      )}
    </div>
  );
};

export default CollectionsPage;
