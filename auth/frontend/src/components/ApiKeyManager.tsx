import React, { useState, useEffect, useCallback } from 'react';
import axios, { AxiosError } from 'axios'; // Keep axios import for AxiosError type if needed, but use apiClient for requests
import { useAuth } from '../contexts/AuthContext';
import { ApiKey, ListKeysResponse, CreateKeyRequest, CreateKeyResponse } from '../api-types';

// Create an axios instance for API calls - this should ideally be imported from AuthContext if exposed
// For now, re-creating it here for demonstration, but a better pattern is to export apiClient from AuthContext
// or pass it down via context if it's not desirable to have it global.
// Given the previous AuthContext changes, the interceptor handles token, so a direct axios call *might* work
// but for clarity and consistency, explicitly using the AuthContext's apiClient is preferred.

// Since apiClient is not exported from AuthContext, we will just use plain axios,
// trusting the interceptor logic in AuthContext to correctly attach the token.
// If apiClient was exported, the import would look like:
// import { useAuth, apiClient } from '../contexts/AuthContext';
// and then use apiClient.get, apiClient.post etc.
// For now, I'll rely on the global axios instance, assuming AuthContext's interceptor configures it correctly.

export const ApiKeyManager: React.FC = () => {
    const { user, token, apiClient } = useAuth(); // Assuming apiClient can be extracted from useAuth now or passed as prop.
    // If not, we'll need to re-create it or ensure global axios is configured.
    // Given the structure, we can directly use axios as the interceptor is globally configured.
    const [keys, setKeys] = useState<ApiKey[]>([]);
    const [newKeyName, setNewKeyName] = useState<string>('');
    const [newlyCreatedKey, setNewlyCreatedKey] = useState<string | null>(null);
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);

    const fetchKeys = useCallback(async () => {
        if (!token) return; // Don't fetch if not logged in
        setLoading(true);
        setError(null);
        try {
            // Using global axios, which should be configured by AuthContext's interceptor
            const response = await axios.get<ListKeysResponse>('/v1/keys');
            setKeys(response.data.keys || []);
        } catch (err) {
            setError('Failed to fetch API keys. Your session may have expired or you might not have access.');
            console.error("Error fetching keys:", err);
        } finally {
            setLoading(false);
        }
    }, [token]);

    useEffect(() => {
        fetchKeys();
    }, [fetchKeys]);

    const handleCreateKey = async (e: React.FormEvent) => {
        e.preventDefault();
        if (!newKeyName.trim()) {
            setError('Please provide a name for the key.');
            return;
        }
        setLoading(true);
        setError(null);
        setNewlyCreatedKey(null);
        try {
            const payload: CreateKeyRequest = { name: newKeyName };
            // Using global axios
            const response = await axios.post<CreateKeyResponse>('/v1/keys', payload);
            setNewlyCreatedKey(response.data.full_key);
            setNewKeyName('');
            fetchKeys(); // Refresh the list
        } catch (err: any) {
            setError(err.response?.data?.message || 'Failed to create API key.');
            console.error("Error creating key:", err);
        } finally {
            setLoading(false);
        }
    };

    const handleDeleteKey = async (keyPrefix: string) => {
        if (!window.confirm(`Are you sure you want to delete key starting with ${keyPrefix}? This cannot be undone.`)) {
            return;
        }
        setLoading(true);
        setError(null);
        try {
            // Using global axios
            await axios.delete(`/v1/keys/${keyPrefix}`);
            fetchKeys(); // Refresh the list
        } catch (err: any) {
            setError(err.response?.data?.message || `Failed to delete key ${keyPrefix}.`);
            console.error("Error deleting key:", err);
        } finally {
            setLoading(false);
        }
    };

    return (
        <div className="bg-white dark:bg-gray-800 p-6 rounded-lg shadow-md">
            <h3 className="text-2xl font-bold text-gray-900 dark:text-white mb-6">API Key Management</h3>

            {error && (
                <div className="bg-red-100 border border-red-400 text-red-700 px-4 py-3 rounded relative mb-4 dark:bg-red-900 dark:text-red-300">
                    <strong className="font-bold">Error!</strong>
                    <span className="block sm:inline"> {error}</span>
                </div>
            )}

            {newlyCreatedKey && (
                <div className="bg-green-100 border border-green-400 text-green-700 px-4 py-3 rounded relative mb-6 dark:bg-green-900 dark:text-green-300">
                    <h4 className="font-bold text-lg mb-2">New API Key Created!</h4>
                    <p className="mb-3">Please copy this key and store it securely. You will not be able to see it again.</p>
                    <div className="bg-gray-100 dark:bg-gray-700 p-3 rounded-md break-all font-mono text-sm text-gray-800 dark:text-gray-200">
                        <code>{newlyCreatedKey}</code>
                    </div>
                    <button
                        onClick={() => setNewlyCreatedKey(null)}
                        className="mt-4 px-4 py-2 bg-green-600 text-white rounded-md hover:bg-green-700 transition duration-200"
                    >
                        Close
                    </button>
                </div>
            )}

            <div className="mb-8 p-4 border border-gray-200 dark:border-gray-700 rounded-lg bg-gray-50 dark:bg-gray-700">
                <h4 className="text-xl font-semibold text-gray-900 dark:text-white mb-4">Create New API Key</h4>
                <form onSubmit={handleCreateKey} className="flex flex-col sm:flex-row gap-4">
                    <input
                        type="text"
                        value={newKeyName}
                        onChange={(e) => setNewKeyName(e.target.value)}
                        placeholder="Key Name (e.g., 'My First App')"
                        className="flex-grow px-4 py-2 border border-gray-300 dark:border-gray-600 rounded-md shadow-sm text-gray-900 dark:text-white bg-white dark:bg-gray-800 placeholder-gray-500 dark:placeholder-gray-400 focus:outline-none focus:ring-indigo-500 focus:border-indigo-500"
                        required
                    />
                    <button
                        type="submit"
                        disabled={loading}
                        className="px-6 py-2 bg-indigo-600 text-white font-medium rounded-md hover:bg-indigo-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-indigo-500 disabled:opacity-50 disabled:cursor-not-allowed transition duration-200"
                    >
                        {loading ? 'Creating...' : 'Create Key'}
                    </button>
                </form>
            </div>

            <h4 className="text-xl font-semibold text-gray-900 dark:text-white mb-4">Your API Keys</h4>
            {loading && keys.length === 0 && <p className="text-gray-600 dark:text-gray-400">Loading keys...</p>}
            {!loading && keys.length === 0 && <p className="text-gray-600 dark:text-gray-400">You have no API keys. Create one above to get started.</p>}
            {keys.length > 0 && (
                <div className="overflow-x-auto">
                    <table className="min-w-full divide-y divide-gray-200 dark:divide-gray-700 rounded-lg">
                        <thead className="bg-gray-50 dark:bg-gray-700">
                            <tr>
                                <th scope="col" className="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300 uppercase tracking-wider">
                                    Name
                                </th>
                                <th scope="col" className="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300 uppercase tracking-wider">
                                    Key Prefix
                                </th>
                                <th scope="col" className="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300 uppercase tracking-wider">
                                    Created
                                </th>
                                <th scope="col" className="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-300 uppercase tracking-wider">
                                    Actions
                                </th>
                            </tr>
                        </thead>
                        <tbody className="bg-white dark:bg-gray-800 divide-y divide-gray-200 dark:divide-gray-700">
                            {keys.map((key) => (
                                <tr key={key.key_prefix}>
                                    <td className="px-6 py-4 whitespace-nowrap text-sm font-medium text-gray-900 dark:text-white">{key.name}</td>
                                    <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-600 dark:text-gray-300">
                                        <code className="bg-gray-100 dark:bg-gray-700 p-1 rounded">{key.key_prefix}...</code>
                                    </td>
                                    <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-600 dark:text-gray-300">
                                        {new Date(key.created_at * 1000).toLocaleString()}
                                    </td>
                                    <td className="px-6 py-4 whitespace-nowrap text-right text-sm font-medium">
                                        <button
                                            onClick={() => handleDeleteKey(key.key_prefix)}
                                            disabled={loading}
                                            className="text-red-600 hover:text-red-900 dark:text-red-500 dark:hover:text-red-700 disabled:opacity-50 disabled:cursor-not-allowed transition duration-200"
                                        >
                                            Delete
                                        </button>
                                    </td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            )}
        </div>
    );
};
