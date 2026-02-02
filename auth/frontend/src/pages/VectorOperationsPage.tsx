import React, { useState, useEffect, useCallback } from "react";
import { useAuth } from "../contexts/AuthContext";
import {
  Database,
  Plus,
  Search,
  Trash2,
  Loader2,
  ArrowUpCircle,
  FileSearch,
  Layers,
  ChevronDown,
} from "lucide-react";
import { Dialog } from "../components/Dialog";
import { Toast } from "../components/Toast";

interface Collection {
  name: string;
  dimension: number;
  count?: number;
}

interface SearchResult {
  id: string;
  score: number;
  vector?: number[];
}

interface VectorData {
  id: string;
  vector: number[];
}

type TabType = "upsert" | "search" | "manage";

const VectorOperationsPage: React.FC = () => {
  const { apiGatewayApiClient } = useAuth();
  const [collections, setCollections] = useState<Collection[]>([]);
  const [selectedCollection, setSelectedCollection] = useState<string>("");
  const [activeTab, setActiveTab] = useState<TabType>("upsert");

  // UI States
  const [isLoading, setIsLoading] = useState(false);
  const [isActionLoading, setIsActionLoading] = useState(false);
  const [toast, setToast] = useState<{
    msg: string;
    type: "success" | "danger";
  } | null>(null);
  const [isDropdownOpen, setIsDropdownOpen] = useState(false);
  const [isDeleteDialogOpen, setIsDeleteDialogOpen] = useState(false);

  // Upsert States
  const [vectorId, setVectorId] = useState("");
  const [vectorValues, setVectorValues] = useState("");

  // Search States
  const [searchVector, setSearchVector] = useState("");
  const [topK, setTopK] = useState<number>(10);
  const [searchResults, setSearchResults] = useState<SearchResult[]>([]);

  // Manage States
  const [manageVectorId, setManageVectorId] = useState("");
  const [foundVector, setFoundVector] = useState<VectorData | null>(null);
  const [deleteVectorId, setDeleteVectorId] = useState("");

  // Fetch collections on mount
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
      if (mapped.length > 0 && !selectedCollection) {
        setSelectedCollection(mapped[0].name);
      }
    } catch (err: any) {
      const msg = err.response?.data?.message || "Failed to load collections";
      setToast({ msg, type: "danger" });
    } finally {
      setIsLoading(false);
    }
  }, [apiGatewayApiClient, selectedCollection]);

  useEffect(() => {
    fetchCollections();
  }, [fetchCollections]);

  // Parse vector string to array of floats
  const parseVectorString = (input: string): number[] => {
    return input
      .split(",")
      .map((v) => v.trim())
      .filter((v) => v !== "")
      .map((v) => parseFloat(v))
      .filter((v) => !isNaN(v));
  };

  // Handle upsert vector
  const handleUpsert = async () => {
    if (!selectedCollection) {
      setToast({ msg: "Please select a collection", type: "danger" });
      return;
    }
    if (!vectorId.trim()) {
      setToast({ msg: "Please enter a vector ID", type: "danger" });
      return;
    }

    const vectorArray = parseVectorString(vectorValues);
    if (vectorArray.length === 0) {
      setToast({ msg: "Please enter valid vector values", type: "danger" });
      return;
    }

    setIsActionLoading(true);
    try {
      await apiGatewayApiClient.post(
        `/v1/collections/${selectedCollection}/points`,
        {
          points: [
            {
              id: vectorId,
              vector: vectorArray,
            },
          ],
        }
      );

      setToast({ msg: "Vector upserted successfully", type: "success" });
      setVectorId("");
      setVectorValues("");
    } catch (err: any) {
      const msg = err.response?.data?.message || "Failed to upsert vector";
      setToast({ msg, type: "danger" });
    } finally {
      setIsActionLoading(false);
    }
  };

  // Handle search vectors
  const handleSearch = async () => {
    if (!selectedCollection) {
      setToast({ msg: "Please select a collection", type: "danger" });
      return;
    }

    const vectorArray = parseVectorString(searchVector);
    if (vectorArray.length === 0) {
      setToast({ msg: "Please enter a valid query vector", type: "danger" });
      return;
    }

    setIsActionLoading(true);
    try {
      const response = await apiGatewayApiClient.post(
        `/v1/collections/${selectedCollection}/search`,
        {
          vector: vectorArray,
          limit: topK,
        }
      );

      const results = response.data.results || [];
      setSearchResults(results);
      if (results.length === 0) {
        setToast({ msg: "No results found", type: "info" as "success" });
      }
    } catch (err: any) {
      const msg = err.response?.data?.message || "Failed to search vectors";
      setToast({ msg, type: "danger" });
    } finally {
      setIsActionLoading(false);
    }
  };

  // Handle get vector by ID
  const handleGetVector = async () => {
    if (!selectedCollection) {
      setToast({ msg: "Please select a collection", type: "danger" });
      return;
    }
    if (!manageVectorId.trim()) {
      setToast({ msg: "Please enter a vector ID", type: "danger" });
      return;
    }

    setIsActionLoading(true);
    try {
      const response = await apiGatewayApiClient.get(
        `/v1/collections/${selectedCollection}/points/${manageVectorId}`
      );

      setFoundVector(response.data);
      setToast({ msg: "Vector found", type: "success" });
    } catch (err: any) {
      const msg = err.response?.data?.message || "Vector not found";
      setToast({ msg, type: "danger" });
      setFoundVector(null);
    } finally {
      setIsActionLoading(false);
    }
  };

  // Handle delete vector
  const handleDeleteVector = async () => {
    if (!selectedCollection || !deleteVectorId) return;

    setIsActionLoading(true);
    try {
      await apiGatewayApiClient.delete(
        `/v1/collections/${selectedCollection}/points/${deleteVectorId}`
      );

      setToast({ msg: "Vector deleted successfully", type: "success" });
      setDeleteVectorId("");
      setIsDeleteDialogOpen(false);
      if (foundVector && foundVector.id === deleteVectorId) {
        setFoundVector(null);
      }
    } catch (err: any) {
      const msg = err.response?.data?.message || "Failed to delete vector";
      setToast({ msg, type: "danger" });
    } finally {
      setIsActionLoading(false);
    }
  };

  const openDeleteDialog = (id: string) => {
    setDeleteVectorId(id);
    setIsDeleteDialogOpen(true);
  };

  const selectedCollectionData = collections.find(
    (c) => c.name === selectedCollection
  );

  return (
    <div className="space-y-6 animate-fade-in">
      {/* Header */}
      <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-white">
            Vector Operations
          </h1>
          <p className="text-neutral-400 mt-1 text-sm">
            Manage vectors in your collections
          </p>
        </div>
        <div className="flex items-center gap-3">
          <button
            onClick={fetchCollections}
            className="p-2 text-neutral-400 hover:text-white transition-colors"
            title="Refresh collections"
          >
            <Loader2
              className={`w-4 h-4 ${isLoading ? "animate-spin" : ""}`}
            />
          </button>
        </div>
      </div>

      {/* Collection Selector */}
      <div className="rounded-xl border border-neutral-800 bg-[#0a0a0a] p-4">
        <label className="block text-xs font-medium text-neutral-400 uppercase mb-2">
          Select Collection
        </label>
        <div className="relative">
          <button
            onClick={() => setIsDropdownOpen(!isDropdownOpen)}
            className="w-full bg-[#050505] border border-neutral-800 rounded-lg px-3 py-2.5 text-sm text-white focus:outline-none focus:border-white transition-all flex items-center justify-between"
          >
            <div className="flex items-center gap-2">
              <Database className="w-4 h-4 text-purple-400" />
              <span>
                {selectedCollectionData
                  ? `${selectedCollectionData.name} (${selectedCollectionData.dimension}D)`
                  : "Select a collection..."}
              </span>
            </div>
            <ChevronDown
              className={`w-4 h-4 text-neutral-500 transition-transform ${
                isDropdownOpen ? "rotate-180" : ""
              }`}
            />
          </button>

          {isDropdownOpen && (
            <>
              <div
                className="fixed inset-0 z-10"
                onClick={() => setIsDropdownOpen(false)}
              />
              <div className="absolute z-20 w-full mt-1 bg-[#0a0a0a] border border-neutral-800 rounded-lg shadow-xl max-h-60 overflow-auto">
                {collections.length === 0 ? (
                  <div className="px-3 py-2 text-sm text-neutral-500">
                    No collections available
                  </div>
                ) : (
                  collections.map((collection) => (
                    <button
                      key={collection.name}
                      onClick={() => {
                        setSelectedCollection(collection.name);
                        setIsDropdownOpen(false);
                      }}
                      className="w-full px-3 py-2 text-sm text-left text-white hover:bg-neutral-800 transition-colors flex items-center gap-2"
                    >
                      <Database className="w-4 h-4 text-neutral-500" />
                      <span>{collection.name}</span>
                      <span className="text-neutral-500 text-xs ml-auto">
                        {collection.dimension}D • {collection.count} vectors
                      </span>
                    </button>
                  ))
                )}
              </div>
            </>
          )}
        </div>
      </div>

      {/* Tabs */}
      <div className="flex gap-1 border-b border-neutral-800">
        {[
          { id: "upsert", label: "Upsert", icon: ArrowUpCircle },
          { id: "search", label: "Search", icon: Search },
          { id: "manage", label: "Get / Delete", icon: FileSearch },
        ].map(({ id, label, icon: Icon }) => (
          <button
            key={id}
            onClick={() => setActiveTab(id as TabType)}
            className={`flex items-center gap-2 px-4 py-3 text-sm font-medium transition-colors border-b-2 -mb-[1px] ${
              activeTab === id
                ? "text-white border-purple-500"
                : "text-neutral-400 border-transparent hover:text-neutral-300"
            }`}
          >
            <Icon className="w-4 h-4" />
            {label}
          </button>
        ))}
      </div>

      {/* Upsert Tab */}
      {activeTab === "upsert" && (
        <div className="rounded-xl border border-neutral-800 bg-[#0a0a0a] p-6 space-y-6">
          <div>
            <label className="block text-xs font-medium text-neutral-400 uppercase mb-2">
              Vector ID
            </label>
            <input
              type="text"
              value={vectorId}
              onChange={(e) => setVectorId(e.target.value)}
              placeholder="Enter vector ID (e.g., doc_001)"
              className="w-full bg-[#050505] border border-neutral-800 rounded-lg px-3 py-2 text-sm text-white placeholder-neutral-600 focus:outline-none focus:border-white transition-all"
            />
          </div>

          <div>
            <label className="block text-xs font-medium text-neutral-400 uppercase mb-2">
              Vector Values
            </label>
            <textarea
              value={vectorValues}
              onChange={(e) => setVectorValues(e.target.value)}
              placeholder="Enter comma-separated float values (e.g., 0.1, 0.2, 0.3...)"
              rows={4}
              className="w-full bg-[#050505] border border-neutral-800 rounded-lg px-3 py-2 text-sm text-white placeholder-neutral-600 focus:outline-none focus:border-white transition-all font-mono resize-none"
            />
            <p className="mt-2 text-xs text-neutral-500">
              Enter {selectedCollectionData?.dimension || "N"} comma-separated
              float values
            </p>
          </div>

          <button
            onClick={handleUpsert}
            disabled={isActionLoading || !selectedCollection}
            className="flex items-center gap-2 bg-white text-black px-4 py-2 rounded-md font-medium text-sm hover:bg-neutral-200 transition-all shadow-[0_0_15px_-3px_rgba(255,255,255,0.3)] disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {isActionLoading ? (
              <Loader2 className="w-4 h-4 animate-spin" />
            ) : (
              <Plus className="w-4 h-4" />
            )}
            {isActionLoading ? "Upserting..." : "Upsert Vector"}
          </button>
        </div>
      )}

      {/* Search Tab */}
      {activeTab === "search" && (
        <div className="space-y-6">
          <div className="rounded-xl border border-neutral-800 bg-[#0a0a0a] p-6 space-y-6">
            <div>
              <label className="block text-xs font-medium text-neutral-400 uppercase mb-2">
                Query Vector
              </label>
              <textarea
                value={searchVector}
                onChange={(e) => setSearchVector(e.target.value)}
                placeholder="Enter comma-separated float values for search query"
                rows={4}
                className="w-full bg-[#050505] border border-neutral-800 rounded-lg px-3 py-2 text-sm text-white placeholder-neutral-600 focus:outline-none focus:border-white transition-all font-mono resize-none"
              />
            </div>

            <div>
              <label className="block text-xs font-medium text-neutral-400 uppercase mb-2">
                Top-K Results
              </label>
              <input
                type="number"
                value={topK}
                onChange={(e) => setTopK(Number(e.target.value))}
                min={1}
                max={100}
                className="w-32 bg-[#050505] border border-neutral-800 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-white transition-all font-mono"
              />
            </div>

            <button
              onClick={handleSearch}
              disabled={isActionLoading || !selectedCollection}
              className="flex items-center gap-2 bg-white text-black px-4 py-2 rounded-md font-medium text-sm hover:bg-neutral-200 transition-all shadow-[0_0_15px_-3px_rgba(255,255,255,0.3)] disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {isActionLoading ? (
                <Loader2 className="w-4 h-4 animate-spin" />
              ) : (
                <Search className="w-4 h-4" />
              )}
              {isActionLoading ? "Searching..." : "Search Vectors"}
            </button>
          </div>

          {/* Search Results */}
          {searchResults.length > 0 && (
            <div className="rounded-xl border border-neutral-800 bg-[#0a0a0a] overflow-hidden">
              <div className="px-6 py-4 border-b border-neutral-800">
                <h3 className="text-sm font-medium text-white">
                  Search Results ({searchResults.length} found)
                </h3>
              </div>
              <div className="overflow-x-auto">
                <table className="min-w-full text-left">
                  <thead>
                    <tr className="border-b border-neutral-800 bg-neutral-900/30">
                      <th className="px-6 py-3 text-xs font-semibold text-neutral-500 uppercase tracking-wider">
                        ID
                      </th>
                      <th className="px-6 py-3 text-xs font-semibold text-neutral-500 uppercase tracking-wider">
                        Score
                      </th>
                      <th className="px-6 py-3 text-xs font-semibold text-neutral-500 uppercase tracking-wider">
                        Vector Preview
                      </th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-neutral-800">
                    {searchResults.map((result) => (
                      <tr key={result.id} className="hover:bg-neutral-900/40">
                        <td className="px-6 py-4 font-mono text-sm text-white">
                          {result.id}
                        </td>
                        <td className="px-6 py-4">
                          <span className="inline-flex items-center px-2 py-1 rounded-full bg-purple-500/10 text-purple-400 text-xs font-medium">
                            {(result.score * 100).toFixed(2)}%
                          </span>
                        </td>
                        <td className="px-6 py-4 text-sm text-neutral-400 font-mono">
                          {result.vector
                            ? `[${result.vector.slice(0, 3).join(", ")}...]`
                            : "N/A"}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}
        </div>
      )}

      {/* Manage Tab */}
      {activeTab === "manage" && (
        <div className="space-y-6">
          <div className="rounded-xl border border-neutral-800 bg-[#0a0a0a] p-6 space-y-6">
            <div>
              <label className="block text-xs font-medium text-neutral-400 uppercase mb-2">
                Vector ID
              </label>
              <div className="flex gap-3">
                <input
                  type="text"
                  value={manageVectorId}
                  onChange={(e) => setManageVectorId(e.target.value)}
                  placeholder="Enter vector ID to fetch"
                  className="flex-1 bg-[#050505] border border-neutral-800 rounded-lg px-3 py-2 text-sm text-white placeholder-neutral-600 focus:outline-none focus:border-white transition-all"
                />
                <button
                  onClick={handleGetVector}
                  disabled={isActionLoading || !selectedCollection}
                  className="flex items-center gap-2 bg-neutral-800 text-white px-4 py-2 rounded-md font-medium text-sm hover:bg-neutral-700 transition-all disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  {isActionLoading ? (
                    <Loader2 className="w-4 h-4 animate-spin" />
                  ) : (
                    <FileSearch className="w-4 h-4" />
                  )}
                  Get Vector
                </button>
              </div>
            </div>
          </div>

          {/* Found Vector */}
          {foundVector && (
            <div className="rounded-xl border border-neutral-800 bg-[#0a0a0a] p-6">
              <div className="flex items-center justify-between mb-4">
                <div className="flex items-center gap-3">
                  <div className="p-2 rounded bg-neutral-800/50 text-purple-400">
                    <Layers className="w-4 h-4" />
                  </div>
                  <div>
                    <h3 className="text-sm font-medium text-white">
                      Vector Found
                    </h3>
                    <p className="text-xs text-neutral-500">ID: {foundVector.id}</p>
                  </div>
                </div>
                <button
                  onClick={() => openDeleteDialog(foundVector.id)}
                  className="flex items-center gap-2 px-3 py-1.5 text-sm text-red-400 hover:text-red-300 hover:bg-red-900/20 rounded transition-colors"
                >
                  <Trash2 className="w-4 h-4" />
                  Delete
                </button>
              </div>

              <div className="bg-[#050505] rounded-lg p-4">
                <label className="block text-xs font-medium text-neutral-400 uppercase mb-2">
                  Vector Data ({foundVector.vector.length} dimensions)
                </label>
                <div className="font-mono text-sm text-neutral-300 break-all max-h-32 overflow-y-auto">
                  [{foundVector.vector.join(", ")}]
                </div>
              </div>
            </div>
          )}
        </div>
      )}

      {/* Delete Confirmation Dialog */}
      <Dialog
        open={isDeleteDialogOpen}
        onClose={() => setIsDeleteDialogOpen(false)}
        title="Delete Vector"
        actions={
          <>
            <button
              onClick={() => setIsDeleteDialogOpen(false)}
              className="px-4 py-2 text-sm text-neutral-400 hover:text-white transition-colors"
            >
              Cancel
            </button>
            <button
              onClick={handleDeleteVector}
              disabled={isActionLoading}
              className="px-4 py-2 bg-red-600 text-white text-sm font-medium rounded hover:bg-red-700 transition-colors disabled:opacity-50 flex items-center gap-2"
            >
              {isActionLoading && (
                <Loader2 className="w-3 h-3 animate-spin" />
              )}
              {isActionLoading ? "Deleting..." : "Delete Vector"}
            </button>
          </>
        }
      >
        <p className="text-neutral-300">
          Are you sure you want to delete the vector with ID{" "}
          <span className="font-mono text-white bg-neutral-800 px-1.5 py-0.5 rounded">
            {deleteVectorId}
          </span>
          ? This action cannot be undone.
        </p>
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

export default VectorOperationsPage;
