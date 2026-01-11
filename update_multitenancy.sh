#!/bin/bash
MODULES=("notification_channel" "tag" "status_page")
SERVER_PATH="apps/server/internal/modules"

for module in "${MODULES[@]}"; do
    echo "Processing $module..."
    
    # Update repository interface
    if [ -f "$SERVER_PATH/$module/${module}.repository.go" ]; then
        sed -i '' 's/FindByID(ctx context.Context, id string)/FindByID(ctx context.Context, id string, orgID string)/' "$SERVER_PATH/$module/${module}.repository.go"
        sed -i '' 's/FindAll(ctx context.Context, page int, limit int, q string)/FindAll(ctx context.Context, page int, limit int, q string, orgID string)/' "$SERVER_PATH/$module/${module}.repository.go"
    fi
    
    echo "  ✓ $module repository updated"
done

echo "All modules updated!"
