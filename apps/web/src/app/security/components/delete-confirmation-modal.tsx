// MARK: - Imports
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

// MARK: - Component Interface
interface DeleteConfirmationModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
  apiKeyName: string;
}

// MARK: - Main Component
const DeleteConfirmationModal = ({
  open,
  onOpenChange,
  onConfirm,
  apiKeyName,
}: DeleteConfirmationModalProps) => {
  // MARK: - Hooks & Render
  const { t } = useLocalizedTranslation();

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {t("security.api_keys.delete_dialog.title")}
          </DialogTitle>
          <DialogDescription>
            {t("security.api_keys.delete_dialog.description", {
              name: apiKeyName,
            })}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
          >
            {t("security.api_keys.delete_dialog.buttons.cancel")}
          </Button>
          <Button
            type="button"
            variant="destructive"
            onClick={() => {
              onConfirm();
              onOpenChange(false);
            }}
          >
            {t("security.api_keys.delete_dialog.buttons.delete")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};

export default DeleteConfirmationModal;
