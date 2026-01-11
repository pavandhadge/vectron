import React from 'react';
import { NavLink, useNavigate } from 'react-router-dom';
import { useAuth } from '../contexts/AuthContext';

export const Navbar = () => {
    const { user, logout } = useAuth();
    const navigate = useNavigate();

    const handleLogout = () => {
        logout();
        navigate('/login'); // Redirect to login page after logout
    };

    const baseLinkClasses = "px-3 py-2 rounded-md text-sm font-medium transition-colors duration-200";
    const activeLinkClasses = "bg-indigo-700 text-white dark:bg-indigo-500";
    const inactiveLinkClasses = "text-gray-300 hover:bg-gray-700 hover:text-white dark:text-gray-300 dark:hover:bg-gray-700 dark:hover:text-white";

    return (
        <nav className="bg-gray-800 dark:bg-gray-900 shadow-lg">
            <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
                <div className="flex items-center justify-between h-16">
                    <div className="flex items-center">
                        <NavLink to="/" className="flex-shrink-0 text-white text-2xl font-bold">
                            Vectron
                        </NavLink>
                        <div className="hidden md:block">
                            <div className="ml-10 flex items-baseline space-x-4">
                                {user ? (
                                    <>
                                        <NavLink
                                            to="/dashboard"
                                            className={({ isActive }) =>
                                                `${baseLinkClasses} ${isActive ? activeLinkClasses : inactiveLinkClasses}`
                                            }
                                        >
                                            Dashboard
                                        </NavLink>
                                    </>
                                ) : null}
                            </div>
                        </div>
                    </div>
                    <div className="hidden md:block">
                        <div className="ml-4 flex items-center md:ml-6">
                            {user ? (
                                <>
                                    <span className="text-gray-300 text-sm mr-4">Welcome, {user.email}!</span>
                                    <button
                                        onClick={handleLogout}
                                        className={`${baseLinkClasses} bg-red-600 hover:bg-red-700 text-white`}
                                    >
                                        Logout
                                    </button>
                                </>
                            ) : (
                                <div className="flex space-x-4">
                                    <NavLink
                                        to="/login"
                                        className={({ isActive }) =>
                                            `${baseLinkClasses} ${isActive ? activeLinkClasses : inactiveLinkClasses}`
                                        }
                                    >
                                        Login
                                    </NavLink>
                                    <NavLink
                                        to="/signup"
                                        className={({ isActive }) =>
                                            `${baseLinkClasses} bg-indigo-600 hover:bg-indigo-700 text-white`
                                        }
                                    >
                                        Sign Up
                                    </NavLink>
                                </div>
                            )}
                        </div>
                    </div>
                    {/* Mobile menu button and responsive menu can be added here if needed */}
                </div>
            </div>
        </nav>
    );
};
