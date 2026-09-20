#include "llama.h"
#include "ggml.h"

#include <algorithm>
#include <chrono>
#include <cstdint>
#include <cstdlib>
#include <cstring>
#include <filesystem>
#include <fstream>
#include <functional>
#include <iomanip>
#include <iostream>
#include <limits>
#include <map>
#include <numeric>
#include <regex>
#include <set>
#include <sstream>
#include <string>
#include <unordered_map>
#include <vector>

namespace {

using Clock = std::chrono::steady_clock;

struct NodeRecord {
    int run = 0;
    int index = 0;
    ggml_tensor * tensor = nullptr;
    std::string layer;
    std::string name;
    std::string op;
    std::string type;
    double duration_us = 0;
};

struct RunSummary {
    int run = 0;
    int token_count = 0;
    int node_count = 0;
    double graph_ms = 0;
    double score = 0;
};

struct ScheduledNode {
    const NodeRecord * node = nullptr;
    int lane = 0;
    double start_us = 0;
    double end_us = 0;
};

struct Profiler {
    bool recording = false;
    int run = 0;
    int next_node = 0;
    Clock::time_point previous;
    std::vector<NodeRecord> nodes;

    static std::string layer_name(const char * raw_name) {
        const std::string name = raw_name == nullptr ? "" : raw_name;
        static const std::regex block_pattern(R"((?:^|\.)blk\.(\d+)(?:\.|$))");
        std::smatch match;
        if (std::regex_search(name, match, block_pattern)) {
            return "blk." + match[1].str();
        }
        if (name.find("token_embd") != std::string::npos || name.find("inp_embd") != std::string::npos) {
            return "embedding";
        }
        if (name.find("output") != std::string::npos || name.find("cls") != std::string::npos || name.find("score") != std::string::npos) {
            return "output";
        }
        return "other";
    }

    static bool callback(ggml_tensor * tensor, bool ask, void * user_data) {
        auto * profiler = static_cast<Profiler *>(user_data);
        if (ask) {
            return true;
        }
        if (!profiler->recording || tensor == nullptr) {
            return true;
        }

        const auto now = Clock::now();
        const double duration_us = std::chrono::duration<double, std::micro>(now - profiler->previous).count();
        profiler->previous = now;

        NodeRecord record;
        record.run = profiler->run;
        record.index = profiler->next_node++;
        record.tensor = tensor;
        record.layer = layer_name(tensor->name);
        record.name = tensor->name == nullptr ? "" : tensor->name;
        record.op = ggml_op_name(tensor->op);
        record.type = ggml_type_name(tensor->type);
        record.duration_us = duration_us;
        profiler->nodes.push_back(std::move(record));
        return true;
    }
};

struct Options {
    std::string model;
    std::string query = "主世界是什么";
    std::string document = "主世界是设定中的最高层级世界，负责统一管理跨维度秩序。";
    std::string output_dir = "build/bench/rerank_graph";
    int runs = 3;
    int warmup = 1;
    int threads = 8;
    int context_size = 1024;
};

void print_usage(const char * executable) {
    std::cerr << "Usage: " << executable << " --model <path> [options]\n"
              << "  --query <text>       Query text\n"
              << "  --document <text>    Document text\n"
              << "  --runs <n>           Measured runs (default: 3)\n"
              << "  --warmup <n>         Warmup runs (default: 1)\n"
              << "  --threads <n>        llama.cpp threads (default: 8)\n"
              << "  --context <n>        Context size (default: 1024)\n"
              << "  --output-dir <dir>   CSV/Graphviz output directory\n";
}

bool take_value(int argc, char ** argv, int & index, std::string & value) {
    if (index + 1 >= argc) {
        return false;
    }
    value = argv[++index];
    return true;
}

bool parse_positive_int(const std::string & text, int & value) {
    try {
        const int parsed = std::stoi(text);
        if (parsed <= 0) {
            return false;
        }
        value = parsed;
        return true;
    } catch (...) {
        return false;
    }
}

bool parse_nonnegative_int(const std::string & text, int & value) {
    try {
        const int parsed = std::stoi(text);
        if (parsed < 0) {
            return false;
        }
        value = parsed;
        return true;
    } catch (...) {
        return false;
    }
}

bool parse_options(int argc, char ** argv, Options & options) {
    for (int i = 1; i < argc; ++i) {
        const std::string argument = argv[i];
        std::string value;
        if (argument == "--model") {
            if (!take_value(argc, argv, i, options.model)) {
                return false;
            }
        } else if (argument == "--query") {
            if (!take_value(argc, argv, i, options.query)) {
                return false;
            }
        } else if (argument == "--document") {
            if (!take_value(argc, argv, i, options.document)) {
                return false;
            }
        } else if (argument == "--output-dir") {
            if (!take_value(argc, argv, i, options.output_dir)) {
                return false;
            }
        } else if (argument == "--runs" || argument == "--warmup" || argument == "--threads" || argument == "--context") {
            if (!take_value(argc, argv, i, value)) {
                return false;
            }
            int parsed = 0;
            const bool valid = argument == "--warmup" ? parse_nonnegative_int(value, parsed) : parse_positive_int(value, parsed);
            if (!valid) {
                return false;
            }
            if (argument == "--runs") {
                options.runs = parsed;
            } else if (argument == "--warmup") {
                options.warmup = parsed;
            } else if (argument == "--threads") {
                options.threads = parsed;
            } else {
                options.context_size = parsed;
            }
        } else if (argument == "--help" || argument == "-h") {
            print_usage(argv[0]);
            std::exit(0);
        } else {
            std::cerr << "Unknown argument: " << argument << "\n";
            return false;
        }
    }
    return !options.model.empty();
}

std::string csv_escape(const std::string & value) {
    std::string escaped = value;
    size_t position = 0;
    while ((position = escaped.find('"', position)) != std::string::npos) {
        escaped.insert(position, 1, '"');
        position += 2;
    }
    return "\"" + escaped + "\"";
}

int block_number(const std::string & layer) {
    if (layer.rfind("blk.", 0) != 0) {
        return -1;
    }
    try {
        return std::stoi(layer.substr(4));
    } catch (...) {
        return -1;
    }
}

std::string explicit_block_layer(const std::string & name) {
    static const std::regex suffix_pattern(R"(-([0-9]+)(?:\s|\(|$))");
    std::smatch match;
    if (!std::regex_search(name, match, suffix_pattern)) {
        return "";
    }
    return "blk." + match[1].str();
}

void assign_layers(std::vector<NodeRecord> & nodes) {
    int current_run = 0;
    std::string current_layer;
    bool output_started = false;
    for (auto & node : nodes) {
        if (node.run != current_run) {
            current_run = node.run;
            current_layer.clear();
            output_started = false;
        }
        const std::string explicit_layer = explicit_block_layer(node.name);
        const bool is_embedding = node.name.find("embd") != std::string::npos || node.name.find("token_types") != std::string::npos;
        const bool is_output = node.name == "result_embd" || node.name == "result_embd_pooled" || node.name.find("cls") != std::string::npos;

        if (is_output) {
            output_started = true;
            node.layer = "output";
            continue;
        }
        if (output_started) {
            node.layer = "output";
            continue;
        }
        if (is_embedding && node.index < 8) {
            node.layer = "embedding";
            continue;
        }
        if (!explicit_layer.empty()) {
            current_layer = explicit_layer;
        } else if (current_layer.empty() && node.index >= 8) {
            current_layer = "blk.0";
        }
        node.layer = current_layer.empty() ? "other" : current_layer;
    }
}

void write_csv_files(const Options & options, const std::vector<NodeRecord> & nodes, const std::vector<RunSummary> & runs) {
    std::ofstream node_file(options.output_dir + "/nodes.csv", std::ios::trunc);
    node_file << "run,node_index,layer,node,op,type,duration_us\n";
    node_file << std::fixed << std::setprecision(3);
    for (const auto & node : nodes) {
        node_file << node.run << ',' << node.index << ',' << csv_escape(node.layer) << ',' << csv_escape(node.name) << ','
                  << csv_escape(node.op) << ',' << csv_escape(node.type) << ',' << node.duration_us << '\n';
    }

    std::map<std::pair<int, std::string>, std::vector<double>> layer_durations;
    for (const auto & node : nodes) {
        layer_durations[{node.run, node.layer}].push_back(node.duration_us);
    }

    std::ofstream layer_file(options.output_dir + "/layers.csv", std::ios::trunc);
    layer_file << "run,layer,node_count,total_us,average_us,max_us\n";
    layer_file << std::fixed << std::setprecision(3);
    std::vector<std::reference_wrapper<const decltype(layer_durations)::value_type>> sorted_layers;
    sorted_layers.reserve(layer_durations.size());
    for (const auto & entry : layer_durations) {
        sorted_layers.push_back(std::cref(entry));
    }
    std::sort(sorted_layers.begin(), sorted_layers.end(), [](const auto & left, const auto & right) {
        const int left_block = block_number(left.get().first.second);
        const int right_block = block_number(right.get().first.second);
        if (left_block >= 0 && right_block >= 0 && left_block != right_block) {
            return left_block < right_block;
        }
        if (left_block >= 0 && right_block < 0) {
            return true;
        }
        if (left_block < 0 && right_block >= 0) {
            return false;
        }
        return left.get().first < right.get().first;
    });
    for (const auto & sorted_entry : sorted_layers) {
        const auto & entry = sorted_entry.get();
        const auto & durations = entry.second;
        const double total = std::accumulate(durations.begin(), durations.end(), 0.0);
        const double maximum = *std::max_element(durations.begin(), durations.end());
        layer_file << entry.first.first << ',' << csv_escape(entry.first.second) << ',' << durations.size() << ',' << total << ','
                   << total / static_cast<double>(durations.size()) << ',' << maximum << '\n';
    }

    std::ofstream run_file(options.output_dir + "/runs.csv", std::ios::trunc);
    run_file << "run,token_count,node_count,graph_ms,score\n";
    run_file << std::fixed << std::setprecision(3);
    for (const auto & run : runs) {
        run_file << run.run << ',' << run.token_count << ',' << run.node_count << ',' << run.graph_ms << ',' << run.score << '\n';
    }
}

std::string dot_escape(const std::string & value) {
    std::string escaped;
    escaped.reserve(value.size() + 8);
    for (const char character : value) {
        switch (character) {
        case '\\':
            escaped += "\\\\";
            break;
        case '"':
            escaped += "\\\"";
            break;
        case '\n':
            escaped += "\\n";
            break;
        case '\r':
            break;
        case '<':
            escaped += "\\<";
            break;
        case '>':
            escaped += "\\>";
            break;
        case '{':
            escaped += "\\{";
            break;
        case '}':
            escaped += "\\}";
            break;
        case '|':
            escaped += "\\|";
            break;
        default:
            escaped += character;
            break;
        }
    }
    return escaped;
}

std::string layer_color(const std::string & layer) {
    const int block = block_number(layer);
    if (layer == "embedding") {
        return "#D9EAF7";
    }
    if (layer == "output") {
        return "#F8D7DA";
    }
    if (block >= 0) {
        static const char * colors[] = {
            "#E8F5E9", "#F1F8E9", "#FFFDE7", "#FFF3E0", "#FBE9E7", "#F3E5F5",
        };
        return colors[block % (sizeof(colors) / sizeof(colors[0]))];
    }
    return "#ECEFF1";
}

std::string layer_sort_key(const std::string & layer) {
    const int block = block_number(layer);
    if (layer == "embedding") {
        return "000000";
    }
    if (block >= 0) {
        std::ostringstream key;
        key << std::setw(6) << std::setfill('0') << block + 1;
        return key.str();
    }
    if (layer == "output") {
        return "999998";
    }
    return "999999" + layer;
}

void write_graphviz_files(const Options & options, const std::vector<NodeRecord> & nodes) {
    std::vector<const NodeRecord *> graph_nodes;
    for (const auto & node : nodes) {
        if (node.run == 1 && node.tensor != nullptr) {
            graph_nodes.push_back(&node);
        }
    }
    if (graph_nodes.empty()) {
        std::cerr << "warning: no measured nodes available for Graphviz export\n";
        return;
    }

    std::unordered_map<const ggml_tensor *, std::string> node_ids;
    node_ids.reserve(graph_nodes.size());
    for (size_t index = 0; index < graph_nodes.size(); ++index) {
        node_ids.emplace(graph_nodes[index]->tensor, "n" + std::to_string(index));
    }

    std::ofstream graph_file(options.output_dir + "/graph.dot", std::ios::trunc);
    graph_file << "digraph rerank_compute_graph {\n"
               << "  graph [rankdir=LR, bgcolor=\"white\", pad=0.2, nodesep=0.12, ranksep=0.55, "
               << "label=\"bge-reranker-v2-m3 compute graph (run 1)\", labelloc=t, fontsize=20];\n"
               << "  node [shape=box, style=\"rounded,filled\", fontname=\"Consolas\", fontsize=8, margin=\"0.08,0.04\"];\n"
               << "  edge [color=\"#78909C\", arrowsize=0.55];\n";

    for (size_t index = 0; index < graph_nodes.size(); ++index) {
        const auto & node = *graph_nodes[index];
        std::ostringstream label;
        label << "#" << node.index << "\\n" << node.layer << "\\n" << node.name << "\\n"
              << node.op << " / " << node.type << "\\n" << std::fixed << std::setprecision(2)
              << node.duration_us << " us";
        graph_file << "  n" << index << " [label=\"" << dot_escape(label.str()) << "\", fillcolor=\""
                   << layer_color(node.layer) << "\"];\n";
    }

    std::unordered_map<const ggml_tensor *, std::string> external_ids;
    std::set<std::pair<std::string, std::string>> edges;
    int external_index = 0;
    for (size_t target_index = 0; target_index < graph_nodes.size(); ++target_index) {
        const auto * node = graph_nodes[target_index];
        const std::string target = node_ids.at(node->tensor);
        for (int source_index = 0; source_index < GGML_MAX_SRC; ++source_index) {
            const ggml_tensor * source = node->tensor->src[source_index];
            if (source == nullptr) {
                continue;
            }
            auto source_it = node_ids.find(source);
            std::string source_id;
            if (source_it != node_ids.end()) {
                const size_t source_node_index = static_cast<size_t>(std::stoi(source_it->second.substr(1)));
                // The callback follows evaluation order.  Ignore in-place/self or
                // stale scheduler links that point forward in that order.
                if (source_node_index >= target_index) {
                    continue;
                }
                source_id = source_it->second;
            } else {
                auto [external_it, inserted] = external_ids.emplace(source, "x" + std::to_string(external_index));
                if (inserted) {
                    ++external_index;
                    std::string name = source->name;
                    if (name.empty()) {
                        name = "constant/input";
                    }
                    graph_file << "  " << external_it->second << " [shape=ellipse, style=\"dashed,filled\", "
                               << "fillcolor=\"#F5F5F5\", label=\"" << dot_escape(name) << "\\n"
                               << dot_escape(ggml_op_name(source->op)) << " / " << dot_escape(ggml_type_name(source->type))
                               << "\"];\n";
                }
                source_id = external_it->second;
            }
            edges.emplace(source_id, target);
        }
    }
    for (const auto & edge : edges) {
        graph_file << "  " << edge.first << " -> " << edge.second << ";\n";
    }
    graph_file << "}\n";
    graph_file.close();

    std::map<std::string, std::pair<int, double>> layer_stats;
    for (const auto * node : graph_nodes) {
        auto & stats = layer_stats[node->layer];
        ++stats.first;
        stats.second += node->duration_us;
    }

    std::vector<std::string> layers;
    layers.reserve(layer_stats.size());
    for (const auto & entry : layer_stats) {
        layers.push_back(entry.first);
    }
    std::sort(layers.begin(), layers.end(), [](const std::string & left, const std::string & right) {
        return layer_sort_key(left) < layer_sort_key(right);
    });
    std::unordered_map<std::string, std::string> layer_ids;
    for (size_t index = 0; index < layers.size(); ++index) {
        layer_ids.emplace(layers[index], "l" + std::to_string(index));
    }

    std::ofstream layer_file(options.output_dir + "/layers.dot", std::ios::trunc);
    layer_file << "digraph rerank_layer_graph {\n"
               << "  graph [rankdir=LR, bgcolor=\"white\", pad=0.3, nodesep=0.35, ranksep=0.8, "
               << "label=\"bge-reranker-v2-m3 layer latency (run 1)\", labelloc=t, fontsize=20];\n"
               << "  node [shape=box, style=\"rounded,filled\", fontname=\"Consolas\", fontsize=11, margin=\"0.15,0.10\"];\n"
               << "  edge [color=\"#546E7A\", arrowsize=0.7, penwidth=1.2];\n";
    for (const auto & layer : layers) {
        const auto & stats = layer_stats.at(layer);
        std::ostringstream label;
        label << layer << "\\n" << stats.first << " nodes\\n" << std::fixed << std::setprecision(2)
              << stats.second / 1000.0 << " ms";
        layer_file << "  " << layer_ids.at(layer) << " [label=\"" << dot_escape(label.str()) << "\", fillcolor=\""
                   << layer_color(layer) << "\"];\n";
    }

    std::set<std::pair<std::string, std::string>> layer_edges;
    for (size_t target_index = 0; target_index < graph_nodes.size(); ++target_index) {
        const auto * node = graph_nodes[target_index];
        const auto target_it = layer_ids.find(node->layer);
        if (target_it == layer_ids.end()) {
            continue;
        }
        for (int source_index = 0; source_index < GGML_MAX_SRC; ++source_index) {
            const ggml_tensor * source = node->tensor->src[source_index];
            if (source == nullptr) {
                continue;
            }
            std::string source_layer;
            const auto source_node_it = node_ids.find(source);
            if (source_node_it != node_ids.end()) {
                const size_t source_node_index = static_cast<size_t>(std::stoi(source_node_it->second.substr(1)));
                if (source_node_index >= target_index) {
                    continue;
                }
                source_layer = graph_nodes[source_node_index]->layer;
            } else {
                // Model weights and constants are external inputs, not layer
                // dependencies.  The full graph keeps them as dashed nodes;
                // the collapsed layer graph only shows compute-to-compute edges.
                continue;
            }
            const auto source_layer_it = layer_ids.find(source_layer);
            if (source_layer_it != layer_ids.end() && source_layer_it->second != target_it->second) {
                layer_edges.emplace(source_layer_it->second, target_it->second);
            }
        }
    }
    for (const auto & edge : layer_edges) {
        layer_file << "  " << edge.first << " -> " << edge.second << ";\n";
    }
    layer_file << "}\n";
    layer_file.close();
}

std::string xml_escape(const std::string & value) {
    std::string escaped;
    escaped.reserve(value.size() + 8);
    for (const char character : value) {
        switch (character) {
        case '&':
            escaped += "&amp;";
            break;
        case '<':
            escaped += "&lt;";
            break;
        case '>':
            escaped += "&gt;";
            break;
        case '"':
            escaped += "&quot;";
            break;
        case '\'':
            escaped += "&apos;";
            break;
        default:
            escaped += character;
            break;
        }
    }
    return escaped;
}

std::vector<ScheduledNode> build_parallel_schedule(const Options & options, const std::vector<NodeRecord> & nodes,
                                                   double & makespan_us, double & total_work_us, int & lane_count) {
    std::vector<const NodeRecord *> graph_nodes;
    for (const auto & node : nodes) {
        if (node.run == 1 && node.tensor != nullptr) {
            graph_nodes.push_back(&node);
        }
    }
    if (graph_nodes.empty()) {
        makespan_us = 0;
        total_work_us = 0;
        lane_count = 0;
        return {};
    }

    lane_count = std::max(1, options.threads);
    std::unordered_map<const ggml_tensor *, size_t> node_indexes;
    node_indexes.reserve(graph_nodes.size());
    for (size_t index = 0; index < graph_nodes.size(); ++index) {
        node_indexes.emplace(graph_nodes[index]->tensor, index);
    }

    std::vector<std::vector<size_t>> dependencies(graph_nodes.size());
    for (size_t target_index = 0; target_index < graph_nodes.size(); ++target_index) {
        const ggml_tensor * tensor = graph_nodes[target_index]->tensor;
        for (int source_index = 0; source_index < GGML_MAX_SRC; ++source_index) {
            const ggml_tensor * source = tensor->src[source_index];
            if (source == nullptr) {
                continue;
            }
            const auto source_it = node_indexes.find(source);
            if (source_it != node_indexes.end() && source_it->second < target_index) {
                dependencies[target_index].push_back(source_it->second);
            }
        }
        std::sort(dependencies[target_index].begin(), dependencies[target_index].end());
        dependencies[target_index].erase(std::unique(dependencies[target_index].begin(), dependencies[target_index].end()),
                                         dependencies[target_index].end());
    }

    std::vector<double> node_end(graph_nodes.size(), 0.0);
    std::vector<double> lane_end(static_cast<size_t>(lane_count), 0.0);
    std::vector<ScheduledNode> schedule;
    schedule.reserve(graph_nodes.size());
    total_work_us = 0;
    for (size_t node_index = 0; node_index < graph_nodes.size(); ++node_index) {
        double dependency_end = 0;
        for (const size_t dependency : dependencies[node_index]) {
            dependency_end = std::max(dependency_end, node_end[dependency]);
        }

        const auto lane_it = std::min_element(lane_end.begin(), lane_end.end());
        const int lane = static_cast<int>(std::distance(lane_end.begin(), lane_it));
        const double start = std::max(dependency_end, lane_end[static_cast<size_t>(lane)]);
        const double duration = std::max(0.001, graph_nodes[node_index]->duration_us);
        const double end = start + duration;
        lane_end[static_cast<size_t>(lane)] = end;
        node_end[node_index] = end;
        total_work_us += duration;
        schedule.push_back(ScheduledNode{graph_nodes[node_index], lane, start, end});
    }

    makespan_us = *std::max_element(lane_end.begin(), lane_end.end());
    return schedule;
}

void write_parallel_csv(const Options & options, const std::vector<ScheduledNode> & schedule) {
    std::ofstream timeline_file(options.output_dir + "/timeline.csv", std::ios::trunc);
    timeline_file << "lane,node_index,layer,node,op,type,start_us,end_us,duration_us\n";
    timeline_file << std::fixed << std::setprecision(3);
    for (const auto & event : schedule) {
        timeline_file << event.lane << ',' << event.node->index << ',' << csv_escape(event.node->layer) << ','
                      << csv_escape(event.node->name) << ',' << csv_escape(event.node->op) << ',' << csv_escape(event.node->type)
                      << ',' << event.start_us << ',' << event.end_us << ',' << event.node->duration_us << '\n';
    }

    std::ofstream folded_file(options.output_dir + "/flame.folded", std::ios::trunc);
    for (const auto & event : schedule) {
        folded_file << "lane_" << event.lane << ';' << event.node->layer << ';' << event.node->op << ';'
                    << "node_" << event.node->index << ' ' << std::max(1LL, static_cast<long long>(event.node->duration_us)) << '\n';
    }
}

void write_parallel_svg(const Options & options, const std::vector<ScheduledNode> & schedule, double makespan_us,
                        double total_work_us, int lane_count, const std::string & filename, const std::string & title,
                        bool flame_style) {
    if (schedule.empty() || makespan_us <= 0 || lane_count <= 0) {
        return;
    }

    constexpr double chart_width = 1600.0;
    constexpr double left_margin = 210.0;
    constexpr double right_margin = 35.0;
    constexpr double top_margin = 72.0;
    constexpr double row_height = 30.0;
    constexpr double bottom_margin = 48.0;
    const double plot_width = chart_width - left_margin - right_margin;
    const double height = top_margin + row_height * lane_count + bottom_margin;
    const double scale = plot_width / makespan_us;
    const double parallelism = total_work_us / makespan_us;

    std::ofstream file(options.output_dir + "/" + filename, std::ios::trunc);
    file << "<?xml version=\"1.0\" encoding=\"UTF-8\" standalone=\"no\"?>\n"
         << "<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"" << chart_width << "\" height=\"" << height
         << "\" viewBox=\"0 0 " << chart_width << ' ' << height << "\">\n"
         << "<rect width=\"100%\" height=\"100%\" fill=\"#ffffff\"/>\n"
         << "<style>text{font-family:Consolas,\"Microsoft YaHei\",sans-serif} .axis{fill:#455a64;font-size:12px} "
         << ".lane{fill:#263238;font-size:13px;font-weight:bold} .meta{fill:#607d8b;font-size:12px} "
         << ".event{stroke:#455a64;stroke-width:0.6}</style>\n"
         << "<text x=\"" << left_margin << "\" y=\"28\" font-size=\"20\" font-weight=\"bold\" fill=\"#102027\">"
         << xml_escape(title) << "</text>\n"
         << "<text x=\"" << left_margin << "\" y=\"49\" class=\"meta\">estimated dependency-aware schedule; "
         << "run 1 measured node durations; not an OS thread trace</text>\n"
         << "<text x=\"" << (chart_width - right_margin) << "\" y=\"28\" text-anchor=\"end\" class=\"meta\">"
         << "work " << std::fixed << std::setprecision(2) << total_work_us / 1000.0 << " ms | makespan "
         << makespan_us / 1000.0 << " ms | parallelism " << parallelism << "x</text>\n";

    for (int lane = 0; lane < lane_count; ++lane) {
        const double y = top_margin + row_height * lane;
        file << "<rect x=\"" << left_margin << "\" y=\"" << y << "\" width=\"" << plot_width << "\" height=\""
             << row_height - 2 << "\" fill=\"" << (lane % 2 == 0 ? "#f7fafb" : "#eef3f5") << "\"/>\n"
             << "<text x=\"" << left_margin - 12 << "\" y=\"" << y + 19 << "\" text-anchor=\"end\" class=\"lane\">"
             << (flame_style ? "stack " : "lane ") << lane << "</text>\n";
    }

    constexpr int tick_count = 8;
    for (int tick = 0; tick <= tick_count; ++tick) {
        const double ratio = static_cast<double>(tick) / tick_count;
        const double x = left_margin + plot_width * ratio;
        file << "<line x1=\"" << x << "\" y1=\"" << top_margin - 4 << "\" x2=\"" << x << "\" y2=\""
             << top_margin + row_height * lane_count << "\" stroke=\"#cfd8dc\" stroke-dasharray=\"2,3\"/>\n"
             << "<text x=\"" << x << "\" y=\"" << (height - 17) << "\" text-anchor=\"middle\" class=\"axis\">"
             << std::fixed << std::setprecision(1) << makespan_us * ratio / 1000.0 << " ms</text>\n";
    }

    for (const auto & event : schedule) {
        const double x = left_margin + event.start_us * scale;
        const double width = std::max(0.7, (event.end_us - event.start_us) * scale);
        const double y = top_margin + row_height * event.lane + 3;
        std::ostringstream tooltip;
        tooltip << '#' << event.node->index << ' ' << event.node->layer << ' ' << event.node->name << ' '
                << event.node->op << " | " << std::fixed << std::setprecision(2) << event.node->duration_us << " us | "
                << event.start_us << "-" << event.end_us << " us";
        file << "<rect class=\"event\" x=\"" << x << "\" y=\"" << y << "\" width=\"" << width
             << "\" height=\"" << row_height - 8 << "\" rx=\"3\" fill=\"" << layer_color(event.node->layer) << "\">"
             << "<title>" << xml_escape(tooltip.str()) << "</title></rect>\n";
        if (width >= 48) {
            file << "<text x=\"" << x + 4 << "\" y=\"" << y + 15 << "\" font-size=\"9\" fill=\"#263238\">"
                 << xml_escape(event.node->op) << "</text>\n";
        }
    }
    file << "</svg>\n";
}

void write_parallel_html(const Options & options, const std::vector<ScheduledNode> & schedule, double makespan_us,
                         double total_work_us, int lane_count) {
    if (schedule.empty() || makespan_us <= 0 || lane_count <= 0) {
        return;
    }

    constexpr double chart_width = 1600.0;
    constexpr double left_margin = 210.0;
    constexpr double right_margin = 35.0;
    constexpr double top_margin = 72.0;
    constexpr double row_height = 30.0;
    constexpr double bottom_margin = 48.0;
    const double plot_width = chart_width - left_margin - right_margin;
    const double height = top_margin + row_height * lane_count + bottom_margin;
    const double scale = plot_width / makespan_us;
    const double parallelism = total_work_us / makespan_us;

    std::map<std::string, std::pair<int, double>> layer_stats;
    for (const auto & event : schedule) {
        auto & stats = layer_stats[event.node->layer];
        ++stats.first;
        stats.second += event.node->duration_us;
    }

    std::ofstream file(options.output_dir + "/parallel_report.html", std::ios::trunc);
    file << "<!doctype html>\n<html lang=\"zh-CN\"><head><meta charset=\"utf-8\">\n"
         << "<meta name=\"viewport\" content=\"width=device-width,initial-scale=1\">\n"
         << "<title>InkFlow Rerank 计算图并行报告</title>\n"
         << "<style>\n"
         << ":root{color-scheme:light;--ink:#173b3b;--muted:#607d8b;--line:#dce7e7;--panel:#fff;--bg:#f3f7f6;--accent:#2f776d;--accent-soft:#e6f2ef;--warn:#fff6e8}\n"
         << "*{box-sizing:border-box}body{margin:0;background:var(--bg);color:#203333;font-family:Segoe UI,Microsoft YaHei,sans-serif}\n"
         << ".page{max-width:1680px;margin:0 auto;padding:28px 30px 50px}.hero{display:flex;justify-content:space-between;gap:24px;align-items:flex-start}\n"
         << "h1{margin:0;color:var(--ink);font-size:28px;letter-spacing:.2px}.subtitle{margin:9px 0 0;color:var(--muted);font-size:14px}\n"
         << ".metric-grid{display:grid;grid-template-columns:repeat(4,minmax(120px,1fr));gap:12px;margin:22px 0}\n"
         << ".metric,.panel{background:var(--panel);border:1px solid var(--line);border-radius:14px;box-shadow:0 4px 16px #173b3b0b}\n"
         << ".metric{padding:15px 17px}.metric b{display:block;font-size:22px;color:var(--ink)}.metric span{display:block;margin-top:4px;color:var(--muted);font-size:12px}\n"
         << ".notice{background:var(--warn);border:1px solid #efd8ad;border-radius:12px;padding:12px 15px;color:#795b2f;font-size:13px;line-height:1.55;margin-bottom:18px}\n"
         << ".tabs{display:flex;gap:8px;margin-bottom:14px}.tab-button{border:1px solid var(--line);background:#fff;color:var(--ink);border-radius:9px;padding:9px 15px;font-size:14px;cursor:pointer}.tab-button.active{background:var(--accent);border-color:var(--accent);color:#fff}\n"
         << ".tab-panel{display:none}.tab-panel.active{display:block}.panel{padding:18px;margin-bottom:18px;overflow:hidden}.panel h2{font-size:17px;margin:0 0 12px;color:var(--ink)}\n"
         << ".chart-wrap{overflow:auto;border:1px solid var(--line);border-radius:10px;background:#fff}.chart{display:block;min-width:1050px;width:100%;height:auto}\n"
         << ".controls{display:flex;gap:10px;align-items:center;margin-bottom:10px}.controls input{flex:1;min-width:220px;border:1px solid var(--line);border-radius:8px;padding:10px 12px;font-size:14px}.count{color:var(--muted);font-size:13px;white-space:nowrap}\n"
         << ".table-wrap{max-height:650px;overflow:auto;border:1px solid var(--line);border-radius:10px}.nodes{width:100%;border-collapse:collapse;font-size:13px;background:#fff}.nodes th{position:sticky;top:0;background:#eef6f4;color:var(--ink);text-align:left;padding:10px 9px;border-bottom:1px solid var(--line);z-index:1}.nodes td{padding:8px 9px;border-bottom:1px solid #edf2f2;white-space:nowrap}.nodes tr:hover{background:#f6fbfa}.nodes tr.selected{background:#fff3d4;outline:2px solid #e6bd60}.nodes .num{text-align:right;font-variant-numeric:tabular-nums}.nodes .op{font-weight:600;color:var(--ink)}.nodes .layer{color:#376f68}.nodes .node-name{max-width:320px;overflow:hidden;text-overflow:ellipsis}\n"
         << ".layers{width:100%;border-collapse:collapse;font-size:13px}.layers th,.layers td{padding:9px;border-bottom:1px solid #edf2f2;text-align:left}.layers th{color:var(--ink);background:#eef6f4}.layers .num{text-align:right;font-variant-numeric:tabular-nums}\n"
         << ".event{cursor:pointer;stroke:#455a64;stroke-width:.6}.event.dim{opacity:.12}.axis{fill:#455a64;font-size:12px}.lane{fill:#263238;font-size:13px;font-weight:bold}.meta{fill:#607d8b;font-size:12px}\n"
         << "@media(max-width:800px){.page{padding:18px 12px}.hero{display:block}.metric-grid{grid-template-columns:repeat(2,1fr)}h1{font-size:23px}}\n"
         << "</style></head><body><main class=\"page\">\n"
         << "<header class=\"hero\"><div><h1>Rerank 计算图并行分析</h1><p class=\"subtitle\">bge-reranker-v2-m3 · 节点顺序、依赖关系、泳道调度与耗时</p></div></header>\n"
         << "<section class=\"metric-grid\">\n"
         << "<div class=\"metric\"><b>" << schedule.size() << "</b><span>计算节点</span></div>\n"
         << "<div class=\"metric\"><b>" << lane_count << "</b><span>推演泳道 / 线程数</span></div>\n"
         << "<div class=\"metric\"><b>" << std::fixed << std::setprecision(2) << total_work_us / 1000.0 << " ms</b><span>节点工作总量</span></div>\n"
         << "<div class=\"metric\"><b>" << makespan_us / 1000.0 << " ms</b><span>依赖调度总时长 · " << parallelism << "x 并行度</span></div>\n"
         << "</section>\n"
         << "<div class=\"notice\"><b>阅读说明：</b>图中横向重叠表示在计算图依赖允许时可以并行；时间轴是根据真实测得的节点耗时和 <code>src[]</code> 依赖做的调度推演。llama.cpp 的评估回调不提供每个 CPU worker 的真实开始/结束时间，因此这不是 OS 线程 trace。</div>\n"
         << "<nav class=\"tabs\"><button class=\"tab-button active\" data-tab=\"swimlane\">并行泳道</button><button class=\"tab-button\" data-tab=\"flame\">火焰时间轴</button><button class=\"tab-button\" data-tab=\"nodes\">节点明细</button></nav>\n";

    auto write_chart = [&](bool flame_style) {
        file << "<section class=\"chart-wrap\"><svg class=\"chart\" xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 "
             << chart_width << ' ' << height << "\" role=\"img\"><rect width=\"100%\" height=\"100%\" fill=\"#fff\"/>\n"
             << "<text x=\"" << left_margin << "\" y=\"28\" font-size=\"20\" font-weight=\"bold\" fill=\"#102027\">"
             << (flame_style ? "Rerank compute graph - parallel flame timeline" : "Rerank compute graph - parallel swimlane") << "</text>\n"
             << "<text x=\"" << left_margin << "\" y=\"49\" class=\"meta\">run 1 measured node durations · click a bar to locate the node below</text>\n"
             << "<text x=\"" << (chart_width - right_margin) << "\" y=\"28\" text-anchor=\"end\" class=\"meta\">work "
             << total_work_us / 1000.0 << " ms | makespan " << makespan_us / 1000.0 << " ms | parallelism " << parallelism << "x</text>\n";
        for (int lane = 0; lane < lane_count; ++lane) {
            const double y = top_margin + row_height * lane;
            file << "<rect x=\"" << left_margin << "\" y=\"" << y << "\" width=\"" << plot_width << "\" height=\""
                 << row_height - 2 << "\" fill=\"" << (lane % 2 == 0 ? "#f7fafb" : "#eef3f5") << "\"/>\n"
                 << "<text x=\"" << left_margin - 12 << "\" y=\"" << y + 19 << "\" text-anchor=\"end\" class=\"lane\">"
                 << (flame_style ? "stack " : "lane ") << lane << "</text>\n";
        }
        constexpr int tick_count = 8;
        for (int tick = 0; tick <= tick_count; ++tick) {
            const double ratio = static_cast<double>(tick) / tick_count;
            const double x = left_margin + plot_width * ratio;
            file << "<line x1=\"" << x << "\" y1=\"" << top_margin - 4 << "\" x2=\"" << x << "\" y2=\""
                 << top_margin + row_height * lane_count << "\" stroke=\"#cfd8dc\" stroke-dasharray=\"2,3\"/>\n"
                 << "<text x=\"" << x << "\" y=\"" << height - 17 << "\" text-anchor=\"middle\" class=\"axis\">"
                 << std::fixed << std::setprecision(1) << makespan_us * ratio / 1000.0 << " ms</text>\n";
        }
        for (const auto & event : schedule) {
            const double x = left_margin + event.start_us * scale;
            const double width = std::max(0.7, (event.end_us - event.start_us) * scale);
            const double y = top_margin + row_height * event.lane + 3;
            std::ostringstream tooltip;
            tooltip << '#' << event.node->index << ' ' << event.node->layer << ' ' << event.node->name << ' '
                    << event.node->op << " | " << std::fixed << std::setprecision(2) << event.node->duration_us << " us | "
                    << event.start_us << "-" << event.end_us << " us";
            file << "<rect class=\"event\" data-node-index=\"" << event.node->index << "\" x=\"" << x << "\" y=\""
                 << y << "\" width=\"" << width << "\" height=\"" << row_height - 8 << "\" rx=\"3\" fill=\""
                 << layer_color(event.node->layer) << "\"><title>" << xml_escape(tooltip.str()) << "</title></rect>\n";
            if (width >= 48) {
                file << "<text x=\"" << x + 4 << "\" y=\"" << y + 15 << "\" font-size=\"9\" fill=\"#263238\">"
                     << xml_escape(event.node->op) << "</text>\n";
            }
        }
        file << "</svg></section>\n";
    };

    file << "<section id=\"tab-swimlane\" class=\"tab-panel active\">";
    write_chart(false);
    file << "</section><section id=\"tab-flame\" class=\"tab-panel\">";
    write_chart(true);
    file << "</section><section id=\"tab-nodes\" class=\"tab-panel\">\n"
         << "<section class=\"panel\"><h2>层级汇总</h2><table class=\"layers\"><thead><tr><th>层</th><th class=\"num\">节点数</th><th class=\"num\">耗时</th></tr></thead><tbody>\n";
    for (const auto & entry : layer_stats) {
        file << "<tr><td>" << xml_escape(entry.first) << "</td><td class=\"num\">" << entry.second.first
             << "</td><td class=\"num\">" << std::fixed << std::setprecision(3) << entry.second.second / 1000.0 << " ms</td></tr>\n";
    }
    file << "</tbody></table></section><section class=\"panel\"><h2>节点顺序与耗时</h2>\n"
         << "<div class=\"controls\"><input id=\"node-filter\" type=\"search\" placeholder=\"搜索节点编号、层、算子或名称…\"><span id=\"node-count\" class=\"count\"></span></div>\n"
         << "<div class=\"table-wrap\"><table class=\"nodes\"><thead><tr><th>#</th><th>层</th><th>节点</th><th>算子</th><th>类型</th><th>泳道</th><th>开始</th><th>结束</th><th>耗时</th></tr></thead><tbody id=\"node-body\">\n";
    for (const auto & event : schedule) {
        std::ostringstream search;
        search << event.node->index << ' ' << event.node->layer << ' ' << event.node->name << ' ' << event.node->op << ' ' << event.node->type;
        file << "<tr id=\"node-row-" << event.node->index << "\" data-search=\"" << xml_escape(search.str()) << "\"><td class=\"num\">"
             << event.node->index << "</td><td class=\"layer\">" << xml_escape(event.node->layer) << "</td><td class=\"node-name\" title=\""
             << xml_escape(event.node->name) << "\">" << xml_escape(event.node->name.empty() ? "(unnamed)" : event.node->name)
             << "</td><td class=\"op\">" << xml_escape(event.node->op) << "</td><td>" << xml_escape(event.node->type)
             << "</td><td class=\"num\">" << event.lane << "</td><td class=\"num\">" << std::fixed << std::setprecision(2)
             << event.start_us << " us</td><td class=\"num\">" << event.end_us << " us</td><td class=\"num\">"
             << event.node->duration_us << " us</td></tr>\n";
    }
    file << "</tbody></table></div></section></section>\n"
         << "</main><script>\n"
         << "const buttons=[...document.querySelectorAll('.tab-button')],panels=[...document.querySelectorAll('.tab-panel')];"
         << "buttons.forEach(b=>b.addEventListener('click',()=>{buttons.forEach(x=>x.classList.toggle('active',x===b));panels.forEach(p=>p.classList.toggle('active',p.id==='tab-'+b.dataset.tab));}));"
         << "const rows=[...document.querySelectorAll('#node-body tr')],bars=[...document.querySelectorAll('.event')],filter=document.querySelector('#node-filter'),count=document.querySelector('#node-count');"
         << "function applyFilter(){const q=(filter.value||'').toLowerCase().trim();let visible=0;rows.forEach(r=>{const yes=!q||r.dataset.search.toLowerCase().includes(q);r.hidden=!yes;if(yes)visible++;});bars.forEach(b=>{const r=document.querySelector('#node-row-'+b.dataset.nodeIndex);b.classList.toggle('dim',!!q&&(!r||r.hidden));});count.textContent=visible+' / '+rows.length+' 个节点';}"
         << "filter.addEventListener('input',applyFilter);applyFilter();"
         << "bars.forEach(b=>b.addEventListener('click',()=>{const r=document.querySelector('#node-row-'+b.dataset.nodeIndex);if(!r)return;buttons.find(x=>x.dataset.tab==='nodes').click();r.scrollIntoView({behavior:'smooth',block:'center'});r.classList.add('selected');setTimeout(()=>r.classList.remove('selected'),1800);}));"
         << "</script></body></html>\n";
}

void write_parallel_visualizations(const Options & options, const std::vector<NodeRecord> & nodes) {
    double makespan_us = 0;
    double total_work_us = 0;
    int lane_count = 0;
    const auto schedule = build_parallel_schedule(options, nodes, makespan_us, total_work_us, lane_count);
    if (schedule.empty()) {
        std::cerr << "warning: no measured nodes available for parallel visualization export\n";
        return;
    }
    write_parallel_csv(options, schedule);
    write_parallel_svg(options, schedule, makespan_us, total_work_us, lane_count, "timeline.svg",
                       "Rerank compute graph - parallel swimlane", false);
    write_parallel_svg(options, schedule, makespan_us, total_work_us, lane_count, "flame.svg",
                       "Rerank compute graph - parallel flame timeline", true);
    write_parallel_html(options, schedule, makespan_us, total_work_us, lane_count);
}

bool tokenize_pair(const llama_vocab * vocab, const std::string & text, std::vector<llama_token> & tokens) {
    tokens.resize(text.size() + 16);
    int32_t count = llama_tokenize(vocab, text.c_str(), static_cast<int32_t>(text.size()), tokens.data(), static_cast<int32_t>(tokens.size()), true, true);
    if (count < 0) {
        tokens.resize(static_cast<size_t>(-count));
        count = llama_tokenize(vocab, text.c_str(), static_cast<int32_t>(text.size()), tokens.data(), static_cast<int32_t>(tokens.size()), true, true);
    }
    if (count < 0) {
        return false;
    }
    tokens.resize(static_cast<size_t>(count));
    return true;
}

bool run_graph(llama_context * context, const std::vector<llama_token> & tokens, Profiler & profiler, int run, double & graph_ms, double & score) {
    llama_memory_clear(llama_get_memory(context), true);

    llama_batch batch = llama_batch_init(static_cast<int32_t>(tokens.size()), 0, 1);
    batch.n_tokens = 0;
    for (size_t i = 0; i < tokens.size(); ++i) {
        batch.token[i] = tokens[i];
        batch.pos[i] = static_cast<llama_pos>(i);
        batch.n_seq_id[i] = 1;
        batch.seq_id[i][0] = 0;
        batch.logits[i] = i + 1 == tokens.size() ? 1 : 0;
        batch.n_tokens = static_cast<int32_t>(i + 1);
    }

    profiler.run = run;
    profiler.next_node = 0;
    profiler.recording = true;
    profiler.previous = Clock::now();
    const auto start = profiler.previous;
    const int result = llama_encode(context, batch);
    const auto end = Clock::now();
    profiler.recording = false;
    llama_batch_free(batch);

    if (result != 0) {
        return false;
    }

    graph_ms = std::chrono::duration<double, std::milli>(end - start).count();
    const float * rerank_output = llama_get_embeddings_seq(context, 0);
    score = rerank_output == nullptr ? 0.0 : rerank_output[0];
    return true;
}

} // namespace

int main(int argc, char ** argv) {
    Options options;
    if (!parse_options(argc, argv, options)) {
        print_usage(argv[0]);
        return 2;
    }

    std::error_code directory_error;
    std::filesystem::create_directories(options.output_dir, directory_error);
    if (directory_error) {
        std::cerr << "failed to create output directory: " << options.output_dir << ": " << directory_error.message() << "\n";
        return 1;
    }

    llama_backend_init();
    llama_model_params model_params = llama_model_default_params();
    llama_model * model = llama_model_load_from_file(options.model.c_str(), model_params);
    if (model == nullptr) {
        std::cerr << "failed to load model: " << options.model << "\n";
        llama_backend_free();
        return 1;
    }

    Profiler profiler;
    llama_context_params context_params = llama_context_default_params();
    context_params.n_ctx = static_cast<uint32_t>(options.context_size);
    context_params.n_batch = static_cast<uint32_t>(options.context_size);
    context_params.n_ubatch = static_cast<uint32_t>(options.context_size);
    context_params.n_threads = options.threads;
    context_params.n_threads_batch = options.threads;
    context_params.embeddings = true;
    context_params.pooling_type = LLAMA_POOLING_TYPE_RANK;
    context_params.cb_eval = Profiler::callback;
    context_params.cb_eval_user_data = &profiler;
    context_params.no_perf = false;

    llama_context * context = llama_init_from_model(model, context_params);
    if (context == nullptr) {
        std::cerr << "failed to create llama context\n";
        llama_model_free(model);
        llama_backend_free();
        return 1;
    }

    const llama_vocab * vocab = llama_model_get_vocab(model);
    const std::string pair = options.query + " " + options.document;
    std::vector<llama_token> tokens;
    if (!tokenize_pair(vocab, pair, tokens)) {
        std::cerr << "failed to tokenize query/document pair\n";
        llama_free(context);
        llama_model_free(model);
        llama_backend_free();
        return 1;
    }

    std::cerr << "model: " << options.model << "\n"
              << "tokens: " << tokens.size() << ", threads: " << options.threads << ", context: " << options.context_size << "\n"
              << "warmup: " << options.warmup << ", runs: " << options.runs << "\n";

    for (int i = 0; i < options.warmup; ++i) {
        double graph_ms = 0;
        double score = 0;
        if (!run_graph(context, tokens, profiler, -1 - i, graph_ms, score)) {
            std::cerr << "warmup decode failed\n";
            llama_free(context);
            llama_model_free(model);
            llama_backend_free();
            return 1;
        }
        profiler.nodes.clear();
    }

    std::vector<RunSummary> summaries;
    for (int run = 1; run <= options.runs; ++run) {
        const size_t node_start = profiler.nodes.size();
        double graph_ms = 0;
        double score = 0;
        if (!run_graph(context, tokens, profiler, run, graph_ms, score)) {
            std::cerr << "measured decode failed at run " << run << "\n";
            llama_free(context);
            llama_model_free(model);
            llama_backend_free();
            return 1;
        }
        const int node_count = static_cast<int>(profiler.nodes.size() - node_start);
        summaries.push_back(RunSummary{run, static_cast<int>(tokens.size()), node_count, graph_ms, score});
        std::cout << "run " << run << ": " << std::fixed << std::setprecision(3) << graph_ms << " ms, " << node_count << " nodes, score=" << score << "\n";
    }

    std::vector<NodeRecord> measured_nodes;
    for (const auto & node : profiler.nodes) {
        if (node.run > 0) {
            measured_nodes.push_back(node);
        }
    }
    assign_layers(measured_nodes);
    write_csv_files(options, measured_nodes, summaries);
    write_graphviz_files(options, measured_nodes);
    write_parallel_visualizations(options, measured_nodes);
    std::cout << "nodes: " << options.output_dir << "/nodes.csv\n"
              << "layers: " << options.output_dir << "/layers.csv\n"
              << "runs: " << options.output_dir << "/runs.csv\n"
              << "graph: " << options.output_dir << "/graph.dot\n"
              << "layers_graph: " << options.output_dir << "/layers.dot\n"
              << "timeline: " << options.output_dir << "/timeline.svg\n"
              << "flame: " << options.output_dir << "/flame.svg\n"
              << "flame_folded: " << options.output_dir << "/flame.folded\n";

    llama_free(context);
    llama_model_free(model);
    llama_backend_free();
    return 0;
}
