-- DELETE /devices/{id} must stop the pushes at once while history rows keep
-- the phone's label (contract v1, DELETE /devices). The label is read from
-- this table, so the row has to stay: the route sets deleted_at instead of
-- deleting. Deleted rows are hidden from GET /devices and skipped when
-- pushing; registering the same FCM token again clears deleted_at.
ALTER TABLE devices ADD COLUMN deleted_at TIMESTAMPTZ;
