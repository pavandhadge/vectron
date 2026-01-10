import React, { useState, useEffect, useCallback, CSSProperties } from 'react';
import axios, { AxiosError } from 'axios';
import { ApiKey, ListKeysResponse, CreateKeyRequest, CreateKeyResponse, DeleteKeyRequest } from './api-types';

interface ApiKeyManagerProps {
    userId: string;
}

export const ApiKeyManager: React.FC<ApiKeyManagerProps> = ({ userId }) => {
    const [keys, setKeys] = useState<ApiKey[]>([]);
    const [newKeyName, setNewKeyName] = useState<string>('');
    const [newlyCreatedKey, setNewlyCreatedKey] = useState<string | null>(null);
    const [loading, setLoading] = useState<boolean>(false);
    const [error, setError] = useState<string | null>(null);

    const fetchKeys = useCallback(async () => {
        if (!userId) return;
        setLoading(true);
        setError(null);
        try {
            const response = await axios.get<ListKeysResponse>(`/v1/users/${userId}/keys`);
            setKeys(response.data.keys || []);
        } catch (err) {
            setError('Failed to fetch API keys. Is the auth service running?');
            console.error(err);
        } finally {
            setLoading(false);
        }
    }, [userId]);

    useEffect(() => {
        fetchKeys();
    }, [fetchKeys]);

    const handleCreateKey = async (e: React.FormEvent) => {
        e.preventDefault();
        if (!newKeyName) {
            setError('Please provide a name for the key.');
            return;
        }
        setLoading(true);
        setError(null);
        setNewlyCreatedKey(null);
        try {
            const payload: CreateKeyRequest = { user_id: userId, name: newKeyName };
            const response = await axios.post<CreateKeyResponse>('/v1/keys', payload);
            setNewlyCreatedKey(response.data.full_key);
            setNewKeyName('');
            fetchKeys(); // Refresh the list
        } catch (err) {
            setError('Failed to create API key.');
            console.error(err);
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
            // The user_id is required in the request body for authorization on the backend
            const payload: DeleteKeyRequest = { user_id: userId };
            await axios.delete(`/v1/keys/${keyPrefix}`, { data: payload });
            fetchKeys(); // Refresh the list
        } catch (err) {
            const axiosError = err as AxiosError;
            if (axiosError.response?.status === 404 || axiosError.response?.status === 500) { // 500 for "not owned by user"
                 setError(`Failed to delete key ${keyPrefix}. Key not found or you do not have permission.`);
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
            {error && <div style={{ color: 'red', marginBottom: '10px', border: '1px solid red', padding: '10px', backgroundColor: '#fff0f0' }}><strong>Error:</strong> {error}</div>}

            {newlyCreatedKey && (
                <div style={{ border: '1px solid green', padding: '10px', marginBottom: '20px', backgroundColor: '#f0fff0' }}>
                    <h4>New API Key Created!</h4>
                    <p>Please copy this key and store it securely. You will not be able to see it again.</p>
                    <pre style={{ backgroundColor: '#eee', padding: '10px', wordWrap: 'break-word' }}>
                        <code>{newlyCreatedKey}</code>
                    </pre>
                    <button onClick={() => setNewlyCreatedKey(null)}>Close</button>
                </div>
            )}

            <div style={{ marginBottom: '20px' }}>
                <h3>Create New API Key</h3>
                <form onSubmit={handleCreateKey}>
                    <input
                        type="text"
                        value={newKeyName}
                        onChange={(e) => setNewKeyName(e.target.value)}
                        placeholder="Key Name (e.g., 'My First App')"
                        style={{ padding: '8px', marginRight: '10px', minWidth: '250px' }}
                    />
                    <button type="submit" disabled={loading} style={{ padding: '8px 12px' }}>
                        {loading ? 'Creating...' : 'Create Key'}
                    </button>
                </form>
            </div>

            <h3>Your API Keys</h3>
            {loading && keys.length === 0 && <p>Loading keys...</p>}
            {!loading && keys.length === 0 && <p>You have no API keys. Create one above.</p>}
            {keys.length > 0 && (
                <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                    <thead>
                        <tr>
                            <th style={tableHeaderStyle}>Name</th>
                            <th style={tableHeaderStyle}>Key Prefix</th>
                            <th style="text-align: left; padding: 8px; border-bottom: 1px solid #ddd; background-color: #f9f9f9;" >Created</th>
                            <th style={tableHeaderStyle}>Actions</th>
                        </tr>
                    </thead>
                    <tbody>
                        {keys.map((key) => (
                            <tr key={key.key_prefix}>
                                <td style={tableCellStyle}>{key.name}</td>
                                <td style={tableCellStyle}><code>{key.key_prefix}...</code></td>
                                <td style={tableCellStyle}>{new Date(key.created_at * 1000).toLocaleString()}</td>
                                <td style={tableCellStyle}>
                                    <button
                                        onClick={() => handleDeleteKey(key.key_prefix)}
                                        disabled={loading}
                                        style={{ color: 'red', border: 'none', background: 'none', cursor: 'pointer' }}
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
    textAlign: 'left',
    padding: '8px',
    borderBottom: '1px solid #ddd',
    backgroundColor: '#f9f9f9',
};

const tableCellStyle: CSSProperties = {
    textAlign: 'left',
    padding: '8px',
    borderBottom: '1px solid #eee',
};
