import React, { useState, useEffect, useCallback } from "react";
import { useAuth } from "../contexts/AuthContext";
import {
  ApiKey,
  ListKeysResponse,
  CreateKeyRequest,
  CreateKeyResponse,
} from "../api-types";
import {
  Copy,
  Trash2,
  Loader2,
  Plus,
  Terminal,
  AlertTriangle,
  Shield,
  Check,
} from "lucide-react";
import { Dialog } from "./Dialog";
import { Toast } from "./Toast";

export const ApiKeyManager: React.FC = () => {
  const { token, apiClient } = useAuth();
  const [keys, setKeys] = useState<ApiKey[]>([]);
  const [newKeyName, setNewKeyName] = useState<string>("");
  const [newlyCreatedKey, setNewlyCreatedKey] = useState<string | null>(null);

  // UI States
  const [loading, setLoading] = useState<boolean>(false);
  const [actionLoading, setActionLoading] = useState<boolean>(false); // For delete/create specific loading
  const [toastMessage, setToastMessage] = useState<{
    message: string;
    type: "success" | "danger" | "info";
  } | null>(null);
  const [copiedKey, setCopiedKey] = useState<string | null>(null); // Visual feedback for copy

  // Dialog States
  const [openDeleteDialog, setOpenDeleteDialog] = useState(false);
  const [openCreateDialog, setOpenCreateDialog] = useState(false);
  const [keyToDelete, setKeyToDelete] = useState<string | null>(null);

  const fetchKeys = useCallback(async () => {
    if (!token) return;
    setLoading(true);
    try {
      const response = await apiClient.get<ListKeysResponse>("/v1/keys");
      setKeys(response.data.keys || []);
    } catch (err) {
      setToastMessage({ message: "Failed to fetch API keys.", type: "danger" });
      console.error("Error fetching keys:", err);
    } finally {
      setLoading(false);
    }
  }, [token, apiClient]);

  useEffect(() => {
    fetchKeys();
  }, [fetchKeys]);

  const handleCreateKey = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newKeyName.trim()) {
      setToastMessage({
        message: "Please provide a name for the key.",
        type: "danger",
      });
      return;
    }
    setActionLoading(true);
    setNewlyCreatedKey(null);
    try {
      const payload: CreateKeyRequest = { name: newKeyName };
      const response = await apiClient.post<CreateKeyResponse>(
        "/v1/keys",
        payload,
      );

      setNewlyCreatedKey(response.data.full_key);
      setNewKeyName("");
      fetchKeys();
      setToastMessage({
        message: "API Key created successfully",
        type: "success",
      });
    } catch (err: any) {
      setToastMessage({
        message: err.response?.data?.message || "Failed to create API key.",
        type: "danger",
      });
    } finally {
      setActionLoading(false);
      setOpenCreateDialog(false);
    }
  };

  const handleDeleteConfirmation = (keyPrefix: string) => {
    setKeyToDelete(keyPrefix);
    setOpenDeleteDialog(true);
  };

  const handleDeleteKey = async () => {
    if (!keyToDelete) return;
    setActionLoading(true);
    try {
      await apiClient.delete(`/v1/keys/${keyToDelete}`);
      setKeys((prev) => prev.filter((k) => k.keyPrefix !== keyToDelete)); // Optimistic update
      setToastMessage({ message: "Key revoked successfully", type: "success" });
    } catch (err: any) {
      setToastMessage({
        message: err.response?.data?.message || "Failed to revoke key.",
        type: "danger",
      });
      fetchKeys(); // Revert on failure
    } finally {
      setActionLoading(false);
      setOpenDeleteDialog(false);
      setKeyToDelete(null);
    }
  };

  const copyToClipboard = (text: string, id: string) => {
    navigator.clipboard.writeText(text);
    setCopiedKey(id);
    setToastMessage({ message: "Copied to clipboard", type: "info" });
    setTimeout(() => setCopiedKey(null), 2000);
  };

  return (
    <div className="space-y-6 animate-fade-in">
      {/* Header */}
      <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4">
        <div>
          <h2 className="text-2xl font-bold tracking-tight text-white">
            API Keys
          </h2>
          <p className="text-neutral-400 mt-1 text-sm">
            Manage authentication tokens for accessing the Vectron API.
          </p>
        </div>
        <button
          className="flex items-center gap-2 bg-white text-black px-4 py-2 rounded-md font-medium text-sm hover:bg-neutral-200 transition-all shadow-[0_0_15px_-3px_rgba(255,255,255,0.3)]"
          onClick={() => setOpenCreateDialog(true)}
        >
          <Plus size={16} strokeWidth={3} />
          Create New Key
        </button>
      </div>

      {/* Table Container */}
      <div className="rounded-xl border border-neutral-800 bg-[#0a0a0a] overflow-hidden shadow-sm">
        <div className="overflow-x-auto">
          <table className="min-w-full text-left">
            <thead>
              <tr className="border-b border-neutral-800 bg-neutral-900/30">
                <th className="px-6 py-4 text-xs font-semibold text-neutral-500 uppercase tracking-wider">
                  Name
                </th>
                <th className="px-6 py-4 text-xs font-semibold text-neutral-500 uppercase tracking-wider">
                  Token Prefix
                </th>
                <th className="px-6 py-4 text-xs font-semibold text-neutral-500 uppercase tracking-wider">
                  Created
                </th>
                <th className="px-6 py-4 text-right text-xs font-semibold text-neutral-500 uppercase tracking-wider">
                  Actions
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-neutral-800">
              {loading && keys.length === 0 ? (
                <tr>
                  <td
                    colSpan={4}
                    className="px-6 py-12 text-center text-neutral-500"
                  >
                    <Loader2
                      className="animate-spin inline-block mb-2"
                      size={24}
                    />
                    <p>Loading keys...</p>
                  </td>
                </tr>
              ) : keys.length > 0 ? (
                keys.map((key) => (
                  <tr
                    key={key.keyPrefix}
                    className="group hover:bg-neutral-900/40 transition-colors"
                  >
                    <td className="px-6 py-4">
                      <div className="flex items-center gap-3">
                        <div className="p-2 rounded bg-neutral-800/50 text-neutral-400">
                          <Terminal size={14} />
                        </div>
                        <span className="font-medium text-white text-sm">
                          {key.name}
                        </span>
                      </div>
                    </td>
                    <td className="px-6 py-4">
                      <div className="inline-flex items-center gap-2 px-2.5 py-1 rounded bg-neutral-900 border border-neutral-800">
                        <span className="font-mono text-xs text-neutral-400 tracking-wide">
                          {key.keyPrefix}••••••••
                        </span>
                      </div>
                    </td>
                    <td className="px-6 py-4 text-sm text-neutral-500">
                      {new Date(key.createdAt * 1000).toLocaleDateString(
                        undefined,
                        {
                          year: "numeric",
                          month: "short",
                          day: "numeric",
                        },
                      )}
                    </td>
                    <td className="px-6 py-4 text-right">
                      <div className="flex justify-end gap-2 opacity-0 group-hover:opacity-100 transition-opacity">
                        <button
                          onClick={() =>
                            copyToClipboard(
                              key.full_key || key.keyPrefix,
                              key.keyPrefix,
                            )
                          }
                          className="p-2 text-neutral-400 hover:text-white hover:bg-neutral-800 rounded-md transition-colors"
                          title="Copy Prefix"
                        >
                          {copiedKey === key.keyPrefix ? (
                            <Check size={16} />
                          ) : (
                            <Copy size={16} />
                          )}
                        </button>
                        <button
                          onClick={() =>
                            handleDeleteConfirmation(key.keyPrefix)
                          }
                          className="p-2 text-neutral-400 hover:text-red-400 hover:bg-red-900/10 rounded-md transition-colors"
                          title="Revoke Key"
                        >
                          <Trash2 size={16} />
                        </button>
                      </div>
                    </td>
                  </tr>
                ))
              ) : (
                <tr>
                  <td colSpan={4} className="px-6 py-16 text-center">
                    <div className="flex flex-col items-center justify-center text-neutral-500">
                      <div className="w-12 h-12 bg-neutral-900 rounded-full flex items-center justify-center mb-4">
                        <Shield className="w-6 h-6 text-neutral-600" />
                      </div>
                      <h3 className="text-white font-medium mb-1">
                        No API keys found
                      </h3>
                      <p className="text-sm max-w-sm">
                        Create a new key to start making authenticated requests
                        to the Vectron API.
                      </p>
                    </div>
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

      {/* --- Dialogs --- */}

      {/* 1. Create Key Dialog */}
      <Dialog
        open={openCreateDialog}
        onClose={() => setOpenCreateDialog(false)}
        title="Create API Key"
        actions={
          <>
            <button
              onClick={() => setOpenCreateDialog(false)}
              className="px-4 py-2 text-sm text-neutral-400 hover:text-white transition-colors"
            >
              Cancel
            </button>
            <button
              onClick={handleCreateKey}
              disabled={actionLoading}
              className="flex items-center gap-2 px-4 py-2 bg-white text-black text-sm font-medium rounded hover:bg-neutral-200 transition-colors disabled:opacity-50"
            >
              {actionLoading && <Loader2 className="animate-spin" size={14} />}
              Create Key
            </button>
          </>
        }
      >
        <div className="space-y-4">
          <p className="text-neutral-400">
            Enter a name for your new API key to identify it later.
          </p>
          <div>
            <label htmlFor="keyName" className="sr-only">
              Key Name
            </label>
            <input
              id="keyName"
              type="text"
              placeholder="e.g. Production Server, Laptop Development..."
              className="w-full bg-[#050505] border border-neutral-800 rounded-lg px-4 py-3 text-sm text-white placeholder-neutral-600 focus:outline-none focus:border-white focus:ring-1 focus:ring-white transition-all"
              value={newKeyName}
              onChange={(e) => setNewKeyName(e.target.value)}
              autoFocus
              onKeyPress={(e) => e.key === "Enter" && handleCreateKey(e)}
            />
          </div>
        </div>
      </Dialog>

      {/* 2. Success/Reveal Dialog */}
      <Dialog
        open={!!newlyCreatedKey}
        onClose={() => setNewlyCreatedKey(null)}
        title="API Key Created"
        actions={
          <button
            onClick={() => setNewlyCreatedKey(null)}
            className="w-full sm:w-auto px-4 py-2 bg-white text-black text-sm font-medium rounded hover:bg-neutral-200 transition-colors"
          >
            I have saved this key
          </button>
        }
      >
        <div className="space-y-4">
          <div className="flex items-start gap-3 p-3 bg-amber-900/10 border border-amber-900/30 rounded-lg">
            <AlertTriangle className="w-5 h-5 text-amber-500 shrink-0" />
            <p className="text-sm text-amber-500">
              This key will only be shown once. If you lose it, you will need to
              create a new one.
            </p>
          </div>

          <div className="relative group">
            <div className="bg-black border border-neutral-800 rounded-lg p-4 font-mono text-sm break-all text-green-400 selection:bg-green-900 selection:text-white">
              {newlyCreatedKey}
            </div>
            <button
              onClick={() =>
                newlyCreatedKey && copyToClipboard(newlyCreatedKey, "new-key")
              }
              className="absolute top-2 right-2 p-2 bg-neutral-800 text-neutral-300 hover:text-white rounded hover:bg-neutral-700 transition-colors"
            >
              {copiedKey === "new-key" ? (
                <Check size={14} />
              ) : (
                <Copy size={14} />
              )}
            </button>
          </div>
        </div>
      </Dialog>

      {/* 3. Delete Confirmation Dialog */}
      <Dialog
        open={openDeleteDialog}
        onClose={() => setOpenDeleteDialog(false)}
        title="Revoke API Key"
        actions={
          <>
            <button
              onClick={() => setOpenDeleteDialog(false)}
              className="px-4 py-2 text-sm text-neutral-400 hover:text-white transition-colors"
            >
              Cancel
            </button>
            <button
              onClick={handleDeleteKey}
              disabled={actionLoading}
              className="flex items-center gap-2 px-4 py-2 bg-red-600/10 text-red-500 border border-red-900/50 text-sm font-medium rounded hover:bg-red-600 hover:text-white transition-all disabled:opacity-50"
            >
              {actionLoading && <Loader2 className="animate-spin" size={14} />}
              Revoke Key
            </button>
          </>
        }
      >
        <p className="text-neutral-400">
          Are you sure you want to revoke the key with prefix{" "}
          <span className="font-mono text-white bg-neutral-800 px-1.5 py-0.5 rounded text-xs">
            {keyToDelete}
          </span>
          ? This action cannot be undone and will immediately block any
          applications using this key.
        </p>
      </Dialog>

      {/* Global Toast */}
      {toastMessage && (
        <Toast
          message={toastMessage.message}
          type={toastMessage.type}
          onClose={() => setToastMessage(null)}
        />
      )}
    </div>
  );
};
