import json
import unittest

import benchmark_agent_sessions as benchmark


class BenchmarkSessionTests(unittest.TestCase):
    def test_question_is_identical_in_both_prompts(self):
        question = "How does the worker handle retries?"
        for arm in ("control", "assisted"):
            task = benchmark.prompt(arm, "omp.exe", question)
            self.assertEqual(task.split("Question: ", 1)[1], question)
            self.assertNotIn(benchmark.QUESTION, task)

    def test_omp_count_ignores_directory_glob(self):
        def event(command):
            return json.dumps({"type": "item.completed", "item": {
                "type": "command_execution", "command": command,
                "exit_code": 0, "aggregated_output": "result"}})

        trace = "\n".join([event("rg --glob '!**/omp/**' worker"),
                           event("& 'C:/tmp/omp-test.exe' investigate worker --json")])
        parsed = benchmark.parse_events(trace, "omp-test.exe")
        self.assertEqual(parsed["command_count"], 2)
        self.assertEqual(parsed["omp_command_count"], 1)

    def test_fast_mode_is_given_only_to_assisted_arm(self):
        self.assertIn("--fast", benchmark.prompt("assisted", "omp.exe", "question", True))
        self.assertNotIn("--fast", benchmark.prompt("control", "omp.exe", "question", True))


if __name__ == "__main__":
    unittest.main()
