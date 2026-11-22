-- 0039: 文件 IO。OI/NOIP 常见题面要求程序读写 test.in / test.out，而不是 stdin/stdout。
-- 两列成对出现：都为空 = 走标准输入输出；只有一边 = 出题表单会拒绝。
-- 判机把用例输入写进 io_input，跑完读 io_output 比较，stdout 不再参与判定。
ALTER TABLE problems ADD COLUMN io_input TEXT NOT NULL DEFAULT '';
ALTER TABLE problems ADD COLUMN io_output TEXT NOT NULL DEFAULT '';
