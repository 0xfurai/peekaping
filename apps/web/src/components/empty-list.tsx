import { Link } from "react-router-dom";

const EmptyList = ({
  title,
  text,
  actionText,
  onClick,
  secondaryActionText,
  secondaryTo,
}: {
  title: string;
  text: string;
  actionText: string;
  onClick: () => void;
  // A second action, shown beside the first, which stays the primary one: a link to another page
  secondaryActionText?: string;
  secondaryTo?: string;
}) => {
  const primary = (
    <button
      onClick={onClick}
      className="inline-flex items-center px-4 py-2 bg-primary text-primary-foreground rounded-md hover:bg-primary/90 transition-colors"
    >
      {actionText}
    </button>
  );
  const secondary =
    secondaryActionText && secondaryTo ? (
      <Link
        to={secondaryTo}
        className="inline-flex items-center px-4 py-2 border border-input bg-background rounded-md hover:bg-accent hover:text-accent-foreground transition-colors"
      >
        {secondaryActionText}
      </Link>
    ) : null;

  return (
    <div className="flex flex-col items-center justify-center py-12 text-center">
      <h3 className="text-lg font-semibold mb-2">{title}</h3>
      <p className="text-muted-foreground mb-6 max-w-sm">{text}</p>
      {secondary ? (
        <div className="flex flex-wrap items-center justify-center gap-2">
          {primary}
          {secondary}
        </div>
      ) : (
        primary
      )}
    </div>
  );
};

export default EmptyList;
