import React from 'react';
import { Outlet } from 'react-router-dom';
import { useAuth } from '../contexts/AuthContext';

export const Dashboard = () => {
    const { user } = useAuth();

    return (
        <div className="min-h-full bg-gray-100 dark:bg-gray-900 py-6 sm:py-8 lg:py-10">
            <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
                <h1 className="text-3xl font-bold text-gray-900 dark:text-white pb-4 mb-6 border-b border-gray-200 dark:border-gray-700">
                    Dashboard
                </h1>
                <p className="text-xl text-gray-700 dark:text-gray-300 mb-8">
                    Welcome, <strong className="text-indigo-600 dark:text-indigo-400">{user?.email}</strong>!
                </p>
                {/* The nested route component will be rendered here */}
                <div className="bg-white dark:bg-gray-800 shadow-md rounded-lg p-6">
                    <Outlet />
                </div>
            </div>
        </div>
    );
};
