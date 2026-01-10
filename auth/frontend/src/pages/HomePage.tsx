import React from 'react';
import { Link } from 'react-router-dom';

export const HomePage = () => {
    const heroStyles: React.CSSProperties = {
        textAlign: 'center',
        padding: '4rem 1rem',
        backgroundColor: 'var(--card-background)',
        borderRadius: '12px',
        border: '1px solid var(--border-color)',
    };

    const h1Style: React.CSSProperties = {
        fontSize: '3.5rem',
        fontWeight: 800,
        margin: '0 0 1rem',
        lineHeight: 1.1,
        letterSpacing: '-0.05rem',
    };

    const pStyle: React.CSSProperties = {
        fontSize: '1.25rem',
        maxWidth: '60ch',
        margin: '0 auto 2rem',
        color: '#b0b0b0',
    };

    return (
        <div style={heroStyles}>
            <h1 style={h1Style}>The Future of Vector Search is Here.</h1>
            <p style={pStyle}>
                Vectron is a blazingly fast, highly-scalable, open-source vector database built for the next generation of AI applications.
            </p>
            <Link to="/signup" style={{fontSize: '1.1rem', padding: '0.8rem 1.5rem'}}>Get Started for Free</Link>
        </div>
    );
};
