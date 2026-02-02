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
import BillingPage from "./pages/BillingPage";
import ProfilePage from "./pages/ProfilePage";
import SDKDownloadPage from "./pages/SDKDownloadPage";
import CollectionsPage from "./pages/CollectionsPage";
import VectorOperationsPage from "./pages/VectorOperationsPage";

// Management Console Pages
import ManagementDashboard from "./pages/ManagementDashboard";
import ApiGatewayManagement from "./pages/ApiGatewayManagement";
import WorkersManagement from "./pages/WorkersManagement";
import CollectionsManagement from "./pages/CollectionsManagement";
import SystemHealthPage from "./pages/SystemHealthPage";

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
            <Route path="profile" element={<ProfilePage />} />
            <Route path="billing" element={<BillingPage />} />
            <Route path="collections" element={<CollectionsPage />} />
            <Route path="vectors" element={<VectorOperationsPage />} />
            <Route path="sdk" element={<SDKDownloadPage />} />
            
            {/* Management Console Routes */}
            <Route path="management" element={<ManagementDashboard />} />
            <Route path="management/gateway" element={<ApiGatewayManagement />} />
            <Route path="management/workers" element={<WorkersManagement />} />
            <Route path="management/collections" element={<CollectionsManagement />} />
            <Route path="management/health" element={<SystemHealthPage />} />
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
