import { Route, Navigate } from "react-router-dom";
import SHLoginPage from "@/app/login/page";
import SHRegisterPage from "@/app/register/page";

export const authRoutes = [
  <Route path="/login" element={<SHLoginPage />} />,
  <Route path="/register" element={<SHRegisterPage />} />,
  <Route path="*" element={<Navigate to="/login" replace />} />
]; 