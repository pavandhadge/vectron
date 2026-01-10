import React from 'react';
import { NavLink, useNavigate } from 'react-router-dom';
import { useAuth } from '../contexts/AuthContext';

export const Navbar = () => {
    const { user, logout } = useAuth();
    const navigate = useNavigate();

    const handleLogout = () => {
        logout();
        navigate('/');
    };

    const linkStyle = {
        color: 'var(--primary-text)',
        textDecoration: 'none',
        padding: '0.5rem 1rem',
        borderRadius: '8px',
        transition: 'background-color 0.2s',
    };

    const activeLinkStyle = {
        ...linkStyle,
        backgroundColor: 'var(--accent-color)',
    };

    return (
        <nav style={{
            display: 'flex',
            justifyContent: 'space-between',
            alignItems: 'center',
            padding: '1rem 2rem',
            backgroundColor: 'var(--card-background)',
            borderBottom: '1px solid var(--border-color)',
        }}>
            <NavLink to="/" style={{...linkStyle, fontSize: '1.5rem', fontWeight: 'bold'}}>
                Vectron
            </NavLink>
            <div style={{ display: 'flex', alignItems: 'center', gap: '1rem' }}>
                {user ? (
                    <>
                        <NavLink
                            to="/dashboard"
                            style={({ isActive }) => isActive ? activeLinkStyle : linkStyle}
                        >
                            Dashboard
                        </NavLink>
                        <span style={{ color: 'var(--secondary-color)' }}>|</span>
                        <button onClick={handleLogout} style={{padding: '0.5rem 1rem', backgroundColor: 'transparent', border: '1px solid var(--secondary-color)'}}>
                            Logout
                        </button>
                    </>
                ) : (
                    <>
                        <NavLink
                            to="/login"
                            style={({ isActive }) => isActive ? activeLinkStyle : linkStyle}
                        >
                            Login
                        </NavLink>
                        <NavLink
                            to="/signup"
                            style={({ isActive }) => isActive ? {...activeLinkStyle, backgroundColor: 'var(--accent-hover)'} : {...linkStyle, backgroundColor: 'var(--accent-color)'} }
                        >
                            Sign Up
                        </NavLink>
                    </>
                )}
            </div>
        </nav>
    );
};
