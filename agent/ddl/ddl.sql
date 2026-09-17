CREATE TABLE IF NOT EXISTS `user` (
  `id` VARCHAR(64) NOT NULL,
  `email` VARCHAR(255) NOT NULL,
  `password` VARCHAR(255) DEFAULT NULL,
  `name` VARCHAR(255) DEFAULT NULL,
  `email_verified` TINYINT(1) NOT NULL DEFAULT 0,
  `image` TEXT DEFAULT NULL,
  `is_anonymous` TINYINT(1) NOT NULL DEFAULT 0,
  `created_at` BIGINT NOT NULL,
  `updated_at` BIGINT NOT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `chat` (
  `id` VARCHAR(64) NOT NULL,
  `created_at` BIGINT NOT NULL,
  `title` VARCHAR(255) NOT NULL,
  `user_id` VARCHAR(64) NOT NULL,
  `visibility` VARCHAR(32) NOT NULL DEFAULT 'private',
  PRIMARY KEY (`id`),
  CONSTRAINT `fk_chat_user` FOREIGN KEY (`user_id`) REFERENCES `user` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `message` (
  `id` VARCHAR(64) NOT NULL,
  `chat_id` VARCHAR(64) NOT NULL,
  `role` VARCHAR(32) NOT NULL,
  `parts` LONGTEXT NOT NULL,
  `attachments` LONGTEXT NOT NULL,
  `created_at` BIGINT NOT NULL,
  PRIMARY KEY (`id`),
  CONSTRAINT `fk_message_chat` FOREIGN KEY (`chat_id`) REFERENCES `chat` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `vote` (
  `chat_id` VARCHAR(64) NOT NULL,
  `message_id` VARCHAR(64) NOT NULL,
  `is_upvoted` TINYINT(1) NOT NULL,
  PRIMARY KEY (`chat_id`, `message_id`),
  CONSTRAINT `fk_vote_chat` FOREIGN KEY (`chat_id`) REFERENCES `chat` (`id`) ON DELETE CASCADE,
  CONSTRAINT `fk_vote_message` FOREIGN KEY (`message_id`) REFERENCES `message` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
