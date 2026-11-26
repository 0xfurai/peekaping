import { Route, Navigate } from "react-router-dom";
import SHLoginPage from "@/app/login/page";
import SHRegisterPage from "@/app/register/page";
import ForgotPasswordPage from "@/app/forgot-password/page";

export const authRoutes = [
  <Route path="/login" element={<SHLoginPage />} />,
  <Route path="/register" element={<SHRegisterPage />} />,
  <Route path="/forgot-password" element={<ForgotPasswordPage />} />,
  <Route path="*" element={<Navigate to="/login" replace />} />
]; 