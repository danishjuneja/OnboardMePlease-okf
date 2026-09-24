# OnboardMePlease 

This project is an attempt to learn Google's OKF and also solve a very common problem most of us face - understanding enterprise level monolithic repos which usually have a ton of different working components. I have spend my fair share of time working with huge monoliths and don't particularly enjoy being clueless about the codebase. 

The basic idea is - Use OKF to group similar files and generate a knowledge base of the repository. Whenever a user wants to get started, or even start working on something they haven't worked on before, instead of pinging codex with their specific problem statement, they can get an overall idea of all the components related to their use case. 

A traditional semantic RAG obviously fails for larger lookups, or cross-module tracing. I also explored Graph-RAG but that again has a lot of compute for indexation. Synthetic docs using OKF seemed like a good idea, as if focusses more on grouping its findings on the domain/business capabilities. 