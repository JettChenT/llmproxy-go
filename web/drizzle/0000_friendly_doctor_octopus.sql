CREATE TABLE `shares` (
	`id` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	`uuid` text NOT NULL,
	`title` text,
	`source_session_id` text NOT NULL,
	`source_listen_addr` text NOT NULL,
	`source_target_url` text NOT NULL,
	`request_count` integer NOT NULL,
	`first_request_id` integer,
	`object_key` text NOT NULL,
	`created_at` integer NOT NULL
);
--> statement-breakpoint
CREATE UNIQUE INDEX `shares_uuid_idx` ON `shares` (`uuid`);