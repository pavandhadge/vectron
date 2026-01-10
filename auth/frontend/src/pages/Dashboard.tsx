import React from 'react';
import { Outlet } from 'react-router-dom';
import { useAuth } from '../contexts/AuthContext';

export const Dashboard = () => {
    const { user } = useAuth();

    return (
        <div>
            <h1 style={{borderBottom: '1px solid var(--border-color)', paddingBottom: '1rem'}}>Dashboard</h1>
            <p style={{fontSize: '1.1rem'}}>Welcome, <strong style={{color: 'var(--accent-color)'}}>{user?.email}</strong>!</p>
            {/* The nested route component will be rendered here */}
            <Outlet />
        </div>
    );
};
