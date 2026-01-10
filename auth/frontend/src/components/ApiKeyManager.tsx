import React, { useState, useEffect, useCallback, CSSProperties } from 'react';
import axios, { AxiosError } from 'axios';
import { useAuth } from '../contexts/AuthContext';
import { ApiKey, ListKeysResponse, CreateKeyRequest, CreateKeyResponse, DeleteKeyRequest } from '../api-types';

export const ApiKeyManager: React.FC = () => {
    const { user, token } = useAuth(); // Get user and token from context
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
            const response = await axios.get<ListKeysResponse>('/v1/keys'); // Endpoint is now protected
            setKeys(response.data.keys || []);
        } catch (err) {
            setError('Failed to fetch API keys. Your session may have expired.');
            console.error(err);
        } finally {
            setLoading(false);
        }
    }, [token]);

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
            const payload: CreateKeyRequest = { name: newKeyName };
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
            // The user ID is now taken from the JWT on the backend, so we don't need to send it.
            await axios.delete(`/v1/keys/${keyPrefix}`);
            fetchKeys(); // Refresh the list
        } catch (err) {
            setError(`Failed to delete key ${keyPrefix}.`);
            console.error(err);
        } finally {
            setLoading(false);
        }
    };

    return (
        <div className="card" style={{marginTop: '2rem'}}>
            <h3 style={{marginTop: 0}}>API Key Management</h3>
            {error && <div style={{ color: 'red', marginBottom: '10px' }}>{error}</div>}

            {newlyCreatedKey && (
                <div style={{ border: '1px solid green', padding: '10px', marginBottom: '20px', backgroundColor: '#1a2e1a' }}>
                    <h4>New API Key Created!</h4>
                    <p>Please copy this key and store it securely. You will not be able to see it again.</p>
                    <pre style={{ backgroundColor: '#0a0a0a', padding: '10px', wordWrap: 'break-word', borderRadius: '8px' }}>
                        <code>{newlyCreatedKey}</code>
                    </pre>
                    <button onClick={() => setNewlyCreatedKey(null)}>Close</button>
                </div>
            )}

            <div style={{ marginBottom: '20px' }}>
                <h4>Create New API Key</h4>
                <form onSubmit={handleCreateKey} style={{display: 'flex', gap: '10px', alignItems: 'center'}}>
                    <input
                        type="text"
                        value={newKeyName}
                        onChange={(e) => setNewKeyName(e.target.value)}
                        placeholder="Key Name (e.g., 'My First App')"
                        style={{ margin: 0 }}
                    />
                    <button type="submit" disabled={loading} style={{ flexShrink: 0 }}>
                        {loading ? 'Creating...' : 'Create Key'}
                    </button>
                </form>
            </div>

            <h4>Your API Keys</h4>
            {loading && keys.length === 0 && <p>Loading keys...</p>}
            {!loading && keys.length === 0 && <p>You have no API keys. Create one above.</p>}
            {keys.length > 0 && (
                <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                    <thead>
                        <tr>
                            <th style={tableHeaderStyle}>Name</th>
                            <th style={tableHeaderStyle}>Key Prefix</th>
                            <th style={tableHeaderStyle}>Created</th>
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
                                        style={{ color: '#ff6b6b', border: 'none', background: 'none', cursor: 'pointer', padding: 0, fontWeight: 'normal' }}
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
    padding: '12px',
    borderBottom: '1px solid var(--border-color)',
    backgroundColor: '#2a2a2a',
};

const tableCellStyle: CSSProperties = {
    textAlign: 'left',
    padding: '12px',
    borderBottom: '1px solid var(--border-color)',
};
