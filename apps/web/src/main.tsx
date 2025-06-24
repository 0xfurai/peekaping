import { createRoot } from "react-dom/client";
import "./index.css";
import App from "./App.tsx";
import { BrowserRouter } from "react-router-dom";
import { Toaster } from "@/components/ui/sonner";
import "./i18n";
import "./i18n/types";

createRoot(document.getElementById("root")!).render(
  <BrowserRouter>
    <App />
    <Toaster richColors />
  </BrowserRouter>
);
