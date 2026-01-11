import React from 'react';
import { Link } from 'react-router-dom';

export const HomePage = () => {
    return (
        <div className="flex flex-col items-center justify-center min-h-[calc(100vh-80px)] px-4 py-12 bg-gray-100 dark:bg-gray-900 text-center">
            <div className="max-w-4xl w-full p-8 space-y-6 bg-white dark:bg-gray-800 rounded-lg shadow-xl border border-gray-200 dark:border-gray-700">
                <h1 className="text-5xl md:text-6xl font-extrabold text-gray-900 dark:text-white leading-tight tracking-tight mb-4">
                    The Future of Vector Search is Here.
                </h1>
                <p className="text-lg md:text-xl text-gray-600 dark:text-gray-300 max-w-2xl mx-auto mb-8">
                    Vectron is a blazingly fast, highly-scalable, open-source vector database built for the next generation of AI applications.
                </p>
                <Link
                    to="/signup"
                    className="inline-flex items-center justify-center px-8 py-3 border border-transparent text-base font-medium rounded-md text-white bg-indigo-600 hover:bg-indigo-700 md:py-4 md:text-lg md:px-10 transition duration-300 ease-in-out transform hover:-translate-y-1 shadow-lg"
                >
                    Get Started for Free
                </Link>
            </div>
        </div>
    );
};
