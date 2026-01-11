import React, { useState, useEffect, useCallback, CSSProperties } from "react";
import { AxiosError } from "axios"; // Keep AxiosError for type checking
import {
  ApiKey,
  ListKeysResponse,
  CreateKeyRequest,
  CreateKeyResponse,
  DeleteKeyRequest,
} from "./api-types";
import { useAuth } from "../contexts/AuthContext"; // Import useAuth

interface ApiKeyManagerProps {
  userId: string;
}

export const ApiKeyManager: React.FC<ApiKeyManagerProps> = ({ userId }) => {
  const { apiClient } = useAuth(); // Get apiClient from AuthContext
  const [keys, setKeys] = useState<ApiKey[]>([]);
  const [newKeyName, setNewKeyName] = useState<string>("");
  const [newlyCreatedKey, setNewlyCreatedKey] = useState<string | null>(null);
  const [loading, setLoading] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);

  const fetchKeys = useCallback(async () => {
    if (!userId) return;
    setLoading(true);
    setError(null);
    try {
      const response = await apiClient.get<ListKeysResponse>(
        `/v1/users/${userId}/keys`,
      ); // Use apiClient
      setKeys(response.data.keys || []);
      if (keys) {
        console.log("hwllo", keys);
        alert(toString(response.data));
      }
    } catch (err) {
      setError("Failed to fetch API keys. Is the auth service running?");
      console.error(err);
    } finally {
      setLoading(false);
    }
  }, [userId, apiClient]); // Add apiClient to dependency array

  useEffect(() => {
    fetchKeys();
  }, [fetchKeys]);

  const handleCreateKey = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newKeyName) {
      setError("Please provide a name for the key.");
      return;
    }
    setLoading(true);
    setError(null);
    setNewlyCreatedKey(null);
    try {
      const payload: CreateKeyRequest = { name: newKeyName }; // Correct payload
      const response = await apiClient.post<CreateKeyResponse>(
        "/v1/keys",
        payload,
      ); // Use apiClient
      setNewlyCreatedKey(response.data.full_key);
      setNewKeyName("");
      fetchKeys(); // Refresh the list
    } catch (err) {
      setError("Failed to create API key.");
      console.error(err);
    } finally {
      setLoading(false);
    }
  };

  const handleDeleteKey = async (keyPrefix: string) => {
    if (
      !window.confirm(
        `Are you sure you want to delete key starting with ${keyPrefix}? This cannot be undone.`,
      )
    ) {
      return;
    }
    setLoading(true);
    setError(null);
    try {
      await apiClient.delete(`/v1/keys/${keyPrefix}`); // Use apiClient, no data payload
      fetchKeys(); // Refresh the list
    } catch (err) {
      const axiosError = err as AxiosError;
      if (
        axiosError.response?.status === 404 ||
        axiosError.response?.status === 500
      ) {
        // 500 for "not owned by user"
        setError(
          `Failed to delete key ${keyPrefix}. Key not found or you do not have permission.`,
        );
      } else {
        setError(`Failed to delete key ${keyPrefix}.`);
      }
      console.error(err);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div>
      {error && (
        <div
          style={{
            color: "red",
            marginBottom: "10px",
            border: "1px solid red",
            padding: "10px",
            backgroundColor: "#fff0f0",
          }}
        >
          <strong>Error:</strong> {error}
        </div>
      )}

      {newlyCreatedKey && (
        <div
          style={{
            border: "1px solid green",
            padding: "10px",
            marginBottom: "20px",
            backgroundColor: "#f0fff0",
          }}
        >
          <h4>New API Key Created!</h4>
          <p>
            Please copy this key and store it securely. You will not be able to
            see it again.
          </p>
          <pre
            style={{
              backgroundColor: "#eee",
              padding: "10px",
              wordWrap: "break-word",
            }}
          >
            <code>{newlyCreatedKey}</code>
          </pre>
          <button onClick={() => setNewlyCreatedKey(null)}>Close</button>
        </div>
      )}

      <div style={{ marginBottom: "20px" }}>
        <h3>Create New API Key</h3>
        <form onSubmit={handleCreateKey}>
          <input
            type="text"
            value={newKeyName}
            onChange={(e) => setNewKeyName(e.target.value)}
            placeholder="Key Name (e.g., 'My First App')"
            style={{ padding: "8px", marginRight: "10px", minWidth: "250px" }}
          />
          <button
            type="submit"
            disabled={loading}
            style={{ padding: "8px 12px" }}
          >
            {loading ? "Creating..." : "Create Key"}
          </button>
        </form>
      </div>

      <h3>Your API Keys</h3>
      {loading && keys.length === 0 && <p>Loading keys...</p>}
      {!loading && keys.length === 0 && (
        <p>You have no API keys. Create one above.</p>
      )}
      {keys.length > 0 && (
        <table style={{ width: "100%", borderCollapse: "collapse" }}>
          <thead>
            <tr>
              <th style={tableCellStyle}>Name</th>
              <th style={tableCellStyle}>Key Prefix</th>
              <th style={tableCellStyle}>Created At</th>
              <th style={tableCellStyle}>Actions</th>
            </tr>
          </thead>

          <tbody>
            {keys.map((key) => (
              <tr key={`${key.keyPrefix}-${key.createdAt}`}>
                <td style={tableCellStyle}>{key.name}</td>

                <td style={tableCellStyle}>
                  <code>{key.keyPrefix}...</code>
                </td>

                <td style={tableCellStyle}>
                  {new Date(Number(key.createdAt) * 1000).toLocaleString()}
                </td>

                <td style={tableCellStyle}>
                  <button
                    onClick={() => handleDeleteKey(key.keyPrefix)}
                    disabled={loading}
                    style={{
                      color: "red",
                      border: "none",
                      background: "none",
                      cursor: "pointer",
                    }}
                  >
                    Delete
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
};

const tableHeaderStyle: CSSProperties = {
  textAlign: "left",
  padding: "8px",
  borderBottom: "1px solid #ddd",
  backgroundColor: "#f9f9f9",
};

const tableCellStyle: CSSProperties = {
  textAlign: "left",
  padding: "8px",
  borderBottom: "1px solid #eee",
};
