import React from "react";
import { BrowserRouter, Routes, Route } from "react-router-dom";
import { AuthProvider } from "./contexts/AuthContext";
import { NewLayout } from "./components/NewLayout";
import { BlankLayout } from "./components/BlankLayout";
import { HomePage } from "./pages/HomePage";
import { LoginPage } from "./pages/LoginPage";
import { SignupPage } from "./pages/SignupPage";
import { Dashboard } from "./pages/Dashboard";
import { ApiKeyManager } from "./components/ApiKeyManager";
import { ProtectedRoute } from "./components/ProtectedRoute";
import { Documentation } from "./pages/Documentation";
import NotYetImplemented from "./pages/NotYetImplemented";

function App() {
  return (
    <AuthProvider>
      <BrowserRouter>
        <Routes>
          <Route path="/" element={<HomePage />} />
          <Route path="/login" element={<LoginPage />} />
          <Route path="/signup" element={<SignupPage />} />
          <Route
            path="/dashboard"
            element={
              <ProtectedRoute>
                <NewLayout />
              </ProtectedRoute>
            }
          >
            <Route index element={<Dashboard />} />
            <Route path="keys" element={<ApiKeyManager />} />
            <Route path="settings" element={<NotYetImplemented />} />
            <Route path="profile" element={<NotYetImplemented />} />
            <Route path="billing" element={<NotYetImplemented />} />
            <Route path="collections" element={<NotYetImplemented />} />
          </Route>
          <Route
            path="/dashboard/documentation"
            element={
              <ProtectedRoute>
                <BlankLayout>
                  <Documentation />
                </BlankLayout>
              </ProtectedRoute>
            }
          />
          <Route path="/*" element={<NotYetImplemented />} />
        </Routes>
      </BrowserRouter>
    </AuthProvider>
  );
}

export default App;
